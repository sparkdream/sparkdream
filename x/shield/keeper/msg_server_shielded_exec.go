package keeper

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"

	errorsmod "cosmossdk.io/errors"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	any "github.com/cosmos/gogoproto/types/any"

	zkcrypto "sparkdream/tools/crypto"
	"sparkdream/x/shield/types"
)

func (k msgServer) ShieldedExec(ctx context.Context, msg *types.MsgShieldedExec) (*types.MsgShieldedExecResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Submitter); err != nil {
		return nil, errorsmod.Wrap(err, "invalid submitter address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	if !params.Enabled {
		return nil, types.ErrShieldDisabled
	}

	// Validate byte lengths of cryptographic fields
	if len(msg.Nullifier) != 0 && len(msg.Nullifier) != 32 {
		return nil, errorsmod.Wrapf(types.ErrInvalidProof, "nullifier must be exactly 32 bytes, got %d", len(msg.Nullifier))
	}
	if len(msg.MerkleRoot) != 0 && len(msg.MerkleRoot) != 32 {
		return nil, errorsmod.Wrapf(types.ErrInvalidProof, "merkle root must be exactly 32 bytes, got %d", len(msg.MerkleRoot))
	}
	if len(msg.Proof) > 1024 {
		return nil, errorsmod.Wrapf(types.ErrInvalidProof, "proof exceeds max size of 1024 bytes, got %d", len(msg.Proof))
	}

	// Route based on execution mode
	switch msg.ExecMode {
	case types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE:
		return k.handleImmediate(sdkCtx, params, msg)
	case types.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH:
		return k.handleEncryptedBatch(sdkCtx, params, msg)
	default:
		return nil, types.ErrInvalidExecMode
	}
}

// immediateExec is an immediate-mode exec that passed validateImmediate.
type immediateExec struct {
	reg types.ShieldedOpRegistration
	// scope is the resolved raw nullifier scope (consume mode).
	scope uint64
	// owner is set for an ownership-mode exec.
	owner *ownedExec
}

// ownedExec is the ownership part of a validated ownership-mode exec.
type ownedExec struct {
	resolver types.ShieldOwnershipResolver
	innerMsg sdk.Msg
	tag      []byte
}

// validateImmediate runs every check of an immediate-mode exec that does not
// write state: registration, batch mode, proof domain, trust level, nullifier
// freshness (or, in ownership mode, that the nullifier is the target's owner
// tag), the identity rate limit and, last, the ZK proof. The ante handler runs it
// before the shield module pays the fee, so an invalid exec costs nothing.
// The proof is skipped when the ante handler already verified this message in
// the same tx (see types.WithProofVerified).
func (k Keeper) validateImmediate(ctx context.Context, params types.Params, msg *types.MsgShieldedExec) (immediateExec, error) {
	// 1. Look up registered operation
	if msg.InnerMessage == nil {
		return immediateExec{}, types.ErrInvalidInnerMessage
	}
	reg, found := k.GetShieldedOp(ctx, msg.InnerMessage.TypeUrl)
	if !found {
		return immediateExec{}, types.ErrUnregisteredOperation
	}
	if !reg.Active {
		return immediateExec{}, types.ErrOperationInactive
	}

	// 2. Validate batch mode allows immediate
	if reg.BatchMode == types.ShieldBatchMode_SHIELD_BATCH_MODE_ENCRYPTED_ONLY {
		return immediateExec{}, types.ErrImmediateNotAllowed
	}

	// 3. Validate proof domain matches registration
	if msg.ProofDomain != reg.ProofDomain {
		return immediateExec{}, types.ErrProofDomainMismatch
	}

	// 4. Validate minimum trust level meets requirement
	if msg.MinTrustLevel < reg.MinTrustLevel {
		return immediateExec{}, types.ErrInsufficientTrustLevel
	}

	// 4b. Every cleartext public input in its one canonical encoding: the
	// verifier reduces mod r, the stores key on raw bytes.
	if err := types.ValidateCanonicalPublicInputs(msg.Nullifier, msg.RateLimitNullifier, msg.MerkleRoot); err != nil {
		return immediateExec{}, err
	}

	// 5. Resolve what the proof is bound to, verify it, and check the nullifier
	v := immediateExec{reg: reg}
	var binding proofBinding
	if reg.NullifierMode == types.NullifierMode_NULLIFIER_MODE_OWNERSHIP {
		owner, claim, err := k.resolveOwnership(ctx, msg)
		if err != nil {
			return immediateExec{}, err
		}
		// The proof must reproduce the owner tag: only the creator's secret
		// key yields it over the owner's scope.
		if !bytes.Equal(msg.Nullifier, claim.Tag) {
			return immediateExec{}, types.ErrOwnershipMismatch
		}
		v.owner = owner
		binding = ownedBinding(claim, msg)
	} else {
		scope, err := k.resolveNullifierScope(ctx, reg, msg)
		if err != nil {
			return immediateExec{}, err
		}
		if k.IsNullifierUsed(ctx, reg.NullifierDomain, scope, hex.EncodeToString(msg.Nullifier)) {
			return immediateExec{}, types.ErrNullifierUsed
		}
		v.scope = scope
		binding = consumeBinding(reg.NullifierDomain, scope, msg)
	}

	// 6. Identity under its rate limit. Checked before the proof, like every
	// other state check, so CheckTx rejects an over-limit exec without a
	// pairing check (the pairing is the expensive step; see the ante
	// handler's proof guard). Either failure rejects the exec, so DeliverTx
	// outcomes are unchanged.
	if k.GetIdentityRateLimitCount(ctx, hex.EncodeToString(msg.RateLimitNullifier)) >= params.MaxExecsPerIdentityPerEpoch {
		return immediateExec{}, types.ErrRateLimitExceeded
	}

	// 7. The target module must accept the message. executeInnerMessage
	// checks this again, but only after the ante has spent the nullifier and
	// the reserve has paid the fee; checked here, an op its module always
	// refuses costs nothing.
	if err := k.checkShieldCompatible(ctx, msg.InnerMessage.TypeUrl, msg.InnerMessage.Value); err != nil {
		return immediateExec{}, err
	}

	// Last, the ZK proof: verifyProof checks the merkle root before the
	// pairing check.
	if !types.ProofVerified(ctx, msg) {
		if err := k.verifyProof(ctx, msg, binding); err != nil {
			return immediateExec{}, err
		}
	}

	return v, nil
}

// checkShieldCompatible refuses an inner message whose module is registered
// as ShieldAware and rejects it. A message that does not decode, or whose
// module registered no gate, is left to executeInnerMessage, which refuses
// both.
func (k Keeper) checkShieldCompatible(ctx context.Context, typeURL string, value []byte) error {
	sa, found := k.getShieldAware(typeURL)
	if !found {
		return nil
	}
	var innerMsg sdk.Msg
	if err := k.cdc.UnpackAny(&codectypes.Any{TypeUrl: typeURL, Value: value}, &innerMsg); err != nil {
		return nil
	}
	if !sa.IsShieldCompatible(ctx, innerMsg) {
		return types.ErrIncompatibleOperation
	}
	return nil
}

// resolveOwnership decodes the inner message and asks its module for the
// ownership claim it acts under.
func (k Keeper) resolveOwnership(ctx context.Context, msg *types.MsgShieldedExec) (*ownedExec, types.OwnershipClaim, error) {
	if msg.InnerMessage.TypeUrl == "" {
		return nil, types.OwnershipClaim{}, types.ErrInvalidInnerMessage
	}
	var innerMsg sdk.Msg
	if err := k.cdc.UnpackAny(&codectypes.Any{TypeUrl: msg.InnerMessage.TypeUrl, Value: msg.InnerMessage.Value}, &innerMsg); err != nil {
		return nil, types.OwnershipClaim{}, errorsmod.Wrap(types.ErrInvalidInnerMessage, err.Error())
	}
	sa, found := k.getShieldAware(msg.InnerMessage.TypeUrl)
	if !found {
		return nil, types.OwnershipClaim{}, types.ErrIncompatibleOperation
	}
	resolver, ok := sa.(types.ShieldOwnershipResolver)
	if !ok {
		return nil, types.OwnershipClaim{}, types.ErrNotOwnable
	}
	claim, err := resolver.ResolveOwnership(ctx, innerMsg)
	if err != nil {
		return nil, types.OwnershipClaim{}, errorsmod.Wrap(types.ErrNotOwnable, err.Error())
	}
	if len(claim.Tag) == 0 {
		return nil, types.OwnershipClaim{}, types.ErrNotOwnable
	}
	return &ownedExec{resolver: resolver, innerMsg: innerMsg, tag: claim.Tag}, claim, nil
}

// PrecheckImmediate validates an immediate-mode exec and, when the context
// runs a real tx (see consumesExec), spends it; it reports whether it did.
// Used by the ante handler before the shield module pays gas.
//
// Spending records the consumed nullifier (or advances the ownership claim's
// sequence) and counts the exec against the identity rate limit, in the ante
// state. In DeliverTx that state is committed with the fee payment even when
// the inner message then fails, so a failing exec costs its member a slot and
// its nullifier exactly like a successful one: it cannot be replayed block
// after block at the reserve's expense, and nobody can re-run it later once
// its target becomes valid (immediate mode now matches batch mode, where the
// nullifier "stays recorded on failure"). In CheckTx the check state is
// discarded at every commit; the mark only stops rewrapped copies of a pending
// exec (anyone can sign as the public submitter) from all entering the
// mempool.
func (k Keeper) PrecheckImmediate(ctx sdk.Context, msg *types.MsgShieldedExec) (bool, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return false, err
	}
	v, err := k.validateImmediate(ctx, params, msg)
	if err != nil {
		return false, err
	}
	if !consumesExec(ctx) {
		return false, nil
	}
	if err := k.consumeImmediate(ctx, params, v, msg); err != nil {
		return false, err
	}
	return true, nil
}

// consumesExec reports whether the ante handler should spend an exec in this
// context: CheckTx/ReCheckTx, the proposal-verification runs and DeliverTx,
// never simulate (its msg server would then find the exec already spent).
func consumesExec(ctx sdk.Context) bool {
	switch ctx.ExecMode() {
	case sdk.ExecModeSimulate:
		return false
	case sdk.ExecModeFinalize, sdk.ExecModePrepareProposal, sdk.ExecModeProcessProposal:
		return true
	default:
		return ctx.IsCheckTx()
	}
}

// consumeImmediate spends a validated immediate exec: it records the
// nullifier (consume mode) or advances the owner's sequence (ownership mode,
// where the nullifier is the reusable owner tag), then counts the exec
// against the identity rate limit.
func (k Keeper) consumeImmediate(ctx sdk.Context, params types.Params, v immediateExec, msg *types.MsgShieldedExec) error {
	if v.owner != nil {
		if err := v.owner.resolver.AdvanceOwnership(ctx, v.owner.innerMsg); err != nil {
			return err
		}
	} else if err := k.RecordNullifier(ctx, v.reg.NullifierDomain, v.scope, hex.EncodeToString(msg.Nullifier), ctx.BlockHeight()); err != nil {
		return err
	}
	if !k.CheckAndIncrementRateLimit(ctx, hex.EncodeToString(msg.RateLimitNullifier), params.MaxExecsPerIdentityPerEpoch) {
		return types.ErrRateLimitExceeded
	}
	return nil
}

// resolveConsumedImmediate rebuilds what the msg server needs to run an exec
// the ante handler already validated and spent in this tx (types.ExecConsumed):
// the registration and the scope, or the ownership resolver and tag. It
// repeats no check, since the nullifier is now recorded and the owner's
// sequence advanced, so the freshness checks would refuse the exec.
func (k Keeper) resolveConsumedImmediate(ctx sdk.Context, msg *types.MsgShieldedExec) (immediateExec, error) {
	if msg.InnerMessage == nil {
		return immediateExec{}, types.ErrInvalidInnerMessage
	}
	reg, found := k.GetShieldedOp(ctx, msg.InnerMessage.TypeUrl)
	if !found {
		return immediateExec{}, types.ErrUnregisteredOperation
	}
	v := immediateExec{reg: reg}
	if reg.NullifierMode == types.NullifierMode_NULLIFIER_MODE_OWNERSHIP {
		owner, _, err := k.resolveOwnership(ctx, msg)
		if err != nil {
			return immediateExec{}, err
		}
		v.owner = owner
		return v, nil
	}
	scope, err := k.resolveNullifierScope(ctx, reg, msg)
	if err != nil {
		return immediateExec{}, err
	}
	v.scope = scope
	return v, nil
}

// handleImmediate executes an immediate-mode exec: validated and spent by the
// ante handler in the same tx, or (reached without the ante, e.g. in tests)
// validated and spent here first.
func (k msgServer) handleImmediate(ctx sdk.Context, params types.Params, msg *types.MsgShieldedExec) (*types.MsgShieldedExecResponse, error) {
	var (
		v   immediateExec
		err error
	)
	if types.ExecConsumed(ctx, msg) {
		v, err = k.resolveConsumedImmediate(ctx, msg)
		if err != nil {
			return nil, err
		}
	} else {
		v, err = k.validateImmediate(ctx, params, msg)
		if err != nil {
			return nil, err
		}
		if err := k.consumeImmediate(ctx, params, v, msg); err != nil {
			return nil, err
		}
	}
	typeURL := msg.InnerMessage.TypeUrl
	nullifierHex := hex.EncodeToString(msg.Nullifier)
	execCtx := types.WithProvenTrustLevel(ctx, msg.MinTrustLevel)

	if v.owner != nil {
		// Ownership: the nullifier is the owner tag and is reused, so it is
		// not recorded; advancing the owner's sequence retired this proof.
		execCtx = types.WithOwnershipTag(execCtx, v.owner.tag)
	} else {
		// A module that lets the anonymous creator manage content later keeps
		// this nullifier as the content's owner tag.
		execCtx = types.WithExecNullifier(execCtx, types.ExecNullifier{
			Domain:    v.reg.NullifierDomain,
			Scope:     v.scope,
			Nullifier: msg.Nullifier,
		})
	}

	// Decode, validate signer, and execute inner message
	resp, err := k.executeInnerMessage(execCtx, params, msg.InnerMessage)
	if err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeShieldedExec,
		sdk.NewAttribute(types.AttributeKeyMessageType, typeURL),
		sdk.NewAttribute(types.AttributeKeyNullifierDomain, fmt.Sprintf("%d", v.reg.NullifierDomain)),
		sdk.NewAttribute(types.AttributeKeyNullifierHex, nullifierHex),
		sdk.NewAttribute(types.AttributeKeyExecMode, "immediate"),
		sdk.NewAttribute(types.AttributeKeyNullifierMode, v.reg.NullifierMode.String()),
	))

	return &types.MsgShieldedExecResponse{InnerResponse: resp}, nil
}

// batchBinding is what a submission proof binds to: the payload-derived
// scope in the batch-submission domain and the hash of the ciphertext.
func batchBinding(msg *types.MsgShieldedExec) proofBinding {
	return proofBinding{
		scope:       zkcrypto.ScopeElement(types.BatchSubmissionDomain, types.BatchSubmissionScope(msg.EncryptedPayload)),
		messageHash: zkcrypto.ShieldMessageHash(types.BatchSubmissionTypeURL, msg.EncryptedPayload),
	}
}

// validateBatch runs every check of an encrypted-batch submission that does
// not write state: batch mode on, no cleartext inner message, payload
// present and within size, 32-byte nullifiers, target epoch current, queue
// headroom, merkle root current, submission nullifier not pending, identity
// under its rate limit and, last, the submission proof (bound to the
// ciphertext, proving the rate-limit nullifier). The ante handler runs it
// before the module pays, so a submission the msg server would reject costs
// the reserve nothing, and the module pays only for proven members under
// their quota.
func (k Keeper) validateBatch(ctx context.Context, params types.Params, msg *types.MsgShieldedExec) error {
	if !params.EncryptedBatchEnabled {
		return types.ErrEncryptedBatchDisabled
	}
	// 1. Reject a cleartext inner message: it would leak the operation.
	if msg.InnerMessage != nil {
		return types.ErrCleartextFieldInBatchMode
	}
	// 2. Validate encrypted payload
	if len(msg.EncryptedPayload) == 0 {
		return types.ErrMissingEncryptedPayload
	}
	if len(msg.EncryptedPayload) > int(params.MaxEncryptedPayloadSize) {
		return types.ErrPayloadTooLarge
	}
	if len(msg.Nullifier) != 32 || len(msg.RateLimitNullifier) != 32 {
		return types.ErrInvalidNullifierLength
	}
	if err := types.ValidateCanonicalPublicInputs(msg.Nullifier, msg.RateLimitNullifier, msg.MerkleRoot); err != nil {
		return err
	}
	// 3. Validate target epoch is current
	epochState, found := k.GetShieldEpochStateVal(ctx)
	if !found {
		return types.ErrEncryptedBatchDisabled
	}
	if msg.TargetEpoch != epochState.CurrentEpoch {
		return types.ErrInvalidTargetEpoch
	}
	// 4. Check pending queue capacity
	if k.GetPendingOpCountVal(ctx) >= uint64(params.MaxPendingQueueSize) {
		return types.ErrPendingQueueFull
	}
	// 5. Submission nullifier not already pending
	if k.IsPendingNullifier(ctx, hex.EncodeToString(msg.Nullifier)) {
		return types.ErrInvalidSubmissionNullifier
	}
	// 6. Identity under its rate limit (proven by the submission proof below)
	if k.GetIdentityRateLimitCount(ctx, hex.EncodeToString(msg.RateLimitNullifier)) >= params.MaxExecsPerIdentityPerEpoch {
		return types.ErrRateLimitExceeded
	}
	// 7. The submission proof: merkle root (checked inside), membership,
	// rate-limit nullifier for the current epoch, bound to this ciphertext.
	if !types.ProofVerified(ctx, msg) {
		if err := k.verifyProof(ctx, msg, batchBinding(msg)); err != nil {
			return err
		}
	}
	return nil
}

// PrecheckExec validates an exec of either mode for the ante handler, before
// the shield module pays gas, and reports whether it also spent the exec in
// this context (immediate mode only; see PrecheckImmediate). In CheckTx (not
// simulate) a batch submission marks its submission nullifier pending in the
// check state, which is discarded at every commit, so rewrapped copies of a
// pending submission don't all enter the mempool. In DeliverTx a batch
// submission is spent by the msg server, which can't fail once validateBatch
// passed against the same state.
func (k Keeper) PrecheckExec(ctx sdk.Context, msg *types.MsgShieldedExec) (bool, error) {
	if msg.ExecMode == types.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH {
		return false, k.precheckBatch(ctx, msg)
	}
	return k.PrecheckImmediate(ctx, msg)
}

func (k Keeper) precheckBatch(ctx sdk.Context, msg *types.MsgShieldedExec) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	if err := k.validateBatch(ctx, params, msg); err != nil {
		return err
	}
	if !ctx.IsCheckTx() || ctx.ExecMode() == sdk.ExecModeSimulate {
		return nil
	}
	return k.RecordPendingNullifier(ctx, hex.EncodeToString(msg.Nullifier))
}

// handleEncryptedBatch validates the submission and queues the encrypted payload.
func (k msgServer) handleEncryptedBatch(ctx sdk.Context, params types.Params, msg *types.MsgShieldedExec) (*types.MsgShieldedExecResponse, error) {
	if err := k.validateBatch(ctx, params, msg); err != nil {
		return nil, err
	}
	epochState, _ := k.GetShieldEpochStateVal(ctx)

	// Record the submission nullifier (released when the op executes or expires)
	nullifierHex := hex.EncodeToString(msg.Nullifier)
	if err := k.RecordPendingNullifier(ctx, nullifierHex); err != nil {
		return nil, err
	}

	// Count against the per-identity rate limit. The rate-limit nullifier was
	// just proven for the current epoch, so this is as unforgeable as in
	// immediate mode.
	if !k.CheckAndIncrementRateLimit(ctx, hex.EncodeToString(msg.RateLimitNullifier), params.MaxExecsPerIdentityPerEpoch) {
		return nil, types.ErrRateLimitExceeded
	}

	// Store pending operation
	opID := k.GetNextPendingOpID(ctx)
	if err := k.SetPendingOp(ctx, types.PendingShieldedOp{
		Id:                  opID,
		TargetEpoch:         msg.TargetEpoch,
		SubmissionNullifier: msg.Nullifier,
		MerkleRoot:          msg.MerkleRoot,
		ProofDomain:         msg.ProofDomain,
		MinTrustLevel:       msg.MinTrustLevel,
		EncryptedPayload:    msg.EncryptedPayload,
		SubmittedAtHeight:   ctx.BlockHeight(),
		SubmittedAtEpoch:    epochState.CurrentEpoch,
		RateLimitNullifier:  msg.RateLimitNullifier,
	}); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeShieldedQueued,
		sdk.NewAttribute(types.AttributeKeyPendingOpId, fmt.Sprintf("%d", opID)),
		sdk.NewAttribute(types.AttributeKeyTargetEpoch, fmt.Sprintf("%d", msg.TargetEpoch)),
		sdk.NewAttribute(types.AttributeKeyNullifierHex, nullifierHex),
		sdk.NewAttribute(types.AttributeKeyExecMode, "encrypted_batch"),
	))

	return &types.MsgShieldedExecResponse{PendingOpId: opID}, nil
}

// resolveNullifierScope determines the scope for nullifier uniqueness.
// A MESSAGE_FIELD scope whose field can't be read is rejected rather than
// falling back to the epoch: the fallback would let one identity act once per
// epoch on something meant to be once per target.
func (k Keeper) resolveNullifierScope(ctx context.Context, reg types.ShieldedOpRegistration, msg *types.MsgShieldedExec) (uint64, error) {
	return k.resolveNullifierScopeAtEpoch(ctx, reg, msg, k.GetCurrentEpoch(ctx))
}

// resolveNullifierScopeAtEpoch is resolveNullifierScope with an explicit epoch
// for the EPOCH scope type. Encrypted-batch ops execute after the epoch has
// advanced, so they must resolve against the epoch the prover bound at
// submission, or the scope public input (and the nullifier's uniqueness
// bucket) silently moves between submission and execution.
func (k Keeper) resolveNullifierScopeAtEpoch(ctx context.Context, reg types.ShieldedOpRegistration, msg *types.MsgShieldedExec, epoch uint64) (uint64, error) {
	switch reg.NullifierScopeType {
	case types.NullifierScopeType_NULLIFIER_SCOPE_EPOCH:
		return reg.EpochScope(epoch), nil
	case types.NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD:
		if msg.InnerMessage != nil && reg.ScopeFieldPath != "" {
			if val, ok := k.extractScope(msg.InnerMessage, reg.ScopeFieldPath); ok {
				return val, nil
			}
		}
		return 0, errorsmod.Wrapf(types.ErrInvalidInnerMessage, "cannot read nullifier scope field %q", reg.ScopeFieldPath)
	case types.NullifierScopeType_NULLIFIER_SCOPE_GLOBAL:
		return 0, nil
	default:
		return epoch, nil
	}
}

// extractScope reads a MESSAGE_FIELD scope: one field, two joined by
// types.ScopeFieldSeparator and packed with types.PackNullifierScope, or two
// joined by types.ScopeFieldFallback (the first when non-zero, else the second).
func (k Keeper) extractScope(msgAny *any.Any, fieldPath string) (uint64, bool) {
	if firstPath, secondPath, ok := strings.Cut(fieldPath, types.ScopeFieldFallback); ok {
		first, ok := extractUint64Field(k.cdc, msgAny, firstPath)
		if !ok {
			return 0, false
		}
		if first != 0 {
			scope, err := types.PackNullifierScope(1, first)
			return scope, err == nil
		}
		second, ok := extractUint64Field(k.cdc, msgAny, secondPath)
		if !ok {
			return 0, false
		}
		scope, err := types.PackNullifierScope(0, second)
		return scope, err == nil
	}
	qualifierPath, idPath, two := strings.Cut(fieldPath, types.ScopeFieldSeparator)
	if !two {
		return extractUint64Field(k.cdc, msgAny, fieldPath)
	}
	qualifier, ok := extractUint64Field(k.cdc, msgAny, qualifierPath)
	if !ok {
		return 0, false
	}
	id, ok := extractUint64Field(k.cdc, msgAny, idPath)
	if !ok {
		return 0, false
	}
	scope, err := types.PackNullifierScope(qualifier, id)
	return scope, err == nil
}

// extractUint64Field extracts a uint64 field from a proto Any message by field name.
// Uses Go reflection on the unpacked gogo proto message to find integer fields.
func extractUint64Field(cdc codec.Codec, msgAny *any.Any, fieldPath string) (uint64, bool) {
	// Unpack the Any into a concrete sdk.Msg
	var innerMsg sdk.Msg
	cdcAny := &codectypes.Any{
		TypeUrl: msgAny.TypeUrl,
		Value:   msgAny.Value,
	}
	if err := cdc.UnpackAny(cdcAny, &innerMsg); err != nil {
		return 0, false
	}

	// Use Go reflection to find the field by proto name.
	// Proto field names use snake_case (e.g. "proposal_id"), but Go struct
	// fields use CamelCase (e.g. "ProposalId"). We check the proto tag on
	// each struct field to match by proto name.
	rv := reflect.ValueOf(innerMsg)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return 0, false
	}

	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		sf := rt.Field(i)
		// Check the protobuf struct tag for the field name
		tag := sf.Tag.Get("protobuf")
		if tag == "" {
			continue
		}
		// Parse proto tag: "varint,1,opt,name=proposal_id,..."
		if !containsProtoName(tag, fieldPath) {
			continue
		}
		// Found the field — extract its value as uint64
		fv := rv.Field(i)
		switch fv.Kind() {
		case reflect.Uint64, reflect.Uint32, reflect.Uint:
			return fv.Uint(), true
		case reflect.Int64, reflect.Int32, reflect.Int:
			v := fv.Int()
			if v >= 0 {
				return uint64(v), true
			}
			return 0, false
		default:
			return 0, false
		}
	}
	return 0, false
}

// containsProtoName checks if a protobuf struct tag contains "name=<fieldName>".
func containsProtoName(tag, fieldName string) bool {
	target := "name=" + fieldName
	for _, part := range strings.Split(tag, ",") {
		if part == target {
			return true
		}
	}
	return false
}

// executeInnerMessage decodes, validates, and dispatches an inner message.
func (k Keeper) executeInnerMessage(ctx sdk.Context, params types.Params, innerMsgAny *any.Any) (*any.Any, error) {
	if k.late.router == nil {
		return nil, types.ErrInvalidInnerMessage
	}

	// Decode inner message
	var innerMsg sdk.Msg
	cdcAny := &codectypes.Any{
		TypeUrl: innerMsgAny.TypeUrl,
		Value:   innerMsgAny.Value,
	}
	if err := k.cdc.UnpackAny(cdcAny, &innerMsg); err != nil {
		return nil, errorsmod.Wrap(types.ErrInvalidInnerMessage, err.Error())
	}

	// Verify every inner message signer is the shield module account.
	// This ensures the inner message was crafted to be executed through the shield,
	// not a regular message that happened to have the same type URL, and that
	// no second signer's authority rides along on the module's.
	moduleAddr := k.accountKeeper.GetModuleAddress(types.ModuleName)
	if legacyMsg, ok := innerMsg.(sdk.LegacyMsg); ok {
		signers := legacyMsg.GetSigners()
		if len(signers) == 0 {
			return nil, types.ErrInvalidInnerMessageSigner
		}
		for _, s := range signers {
			if !s.Equals(moduleAddr) {
				return nil, types.ErrInvalidInnerMessageSigner
			}
		}
	} else if pc, ok := k.cdc.(*codec.ProtoCodec); ok {
		signerBytes, _, err := pc.GetMsgV1Signers(innerMsg)
		if err != nil || len(signerBytes) == 0 {
			return nil, types.ErrInvalidInnerMessageSigner
		}
		for _, s := range signerBytes {
			if !sdk.AccAddress(s).Equals(moduleAddr) {
				return nil, types.ErrInvalidInnerMessageSigner
			}
		}
	} else {
		// Neither path could validate the inner signer — fail closed instead of
		// relying on the operation whitelist alone.
		return nil, errorsmod.Wrap(types.ErrInvalidInnerMessageSigner, "cannot extract inner message signer for validation")
	}

	// Check ShieldAware gate: target module MUST explicitly opt in.
	// Both the governance whitelist (registered operation) AND the ShieldAware gate
	// must pass. If a module doesn't register a ShieldAware implementation, execution
	// is rejected — this prevents accidental bypass when a module forgets to register.
	sa, found := k.getShieldAware(innerMsgAny.TypeUrl)
	if !found {
		return nil, types.ErrIncompatibleOperation
	}
	if !sa.IsShieldCompatible(ctx, innerMsg) {
		return nil, types.ErrIncompatibleOperation
	}

	// Dispatch via router
	handler := k.late.router.Handler(innerMsg)
	if handler == nil {
		return nil, types.ErrIncompatibleOperation
	}

	// Execute under a per-exec gas cap, then charge whatever the inner
	// handler used to the outer tx so the fee and the block gas accounting
	// cover the inner work, not just the dispatch.
	childCtx := ctx.WithGasMeter(storetypes.NewGasMeter(params.MaxGasPerExec))
	result, err := handler(childCtx, innerMsg)
	ctx.GasMeter().ConsumeGas(childCtx.GasMeter().GasConsumed(), "shield: inner message")
	if err != nil {
		return nil, err
	}
	// The router runs the handler on a fresh event manager; re-emit its
	// events (e.g. collection_created, coin_spent) on the exec's. They name
	// the shield address as actor, never the member.
	if result != nil {
		ctx.EventManager().EmitEvents(result.GetEvents())
	}

	// Return the first response message as Any, if present
	if result != nil && len(result.MsgResponses) > 0 {
		resp := result.MsgResponses[0]
		return &any.Any{
			TypeUrl: resp.TypeUrl,
			Value:   resp.Value,
		}, nil
	}
	return nil, nil
}
