package keeper

import (
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

// handleImmediate verifies the ZK proof and executes the inner message immediately.
func (k msgServer) handleImmediate(ctx sdk.Context, params types.Params, msg *types.MsgShieldedExec) (*types.MsgShieldedExecResponse, error) {
	// 1. Look up registered operation
	if msg.InnerMessage == nil {
		return nil, types.ErrInvalidInnerMessage
	}
	typeURL := msg.InnerMessage.TypeUrl
	reg, found := k.GetShieldedOp(ctx, typeURL)
	if !found {
		return nil, types.ErrUnregisteredOperation
	}
	if !reg.Active {
		return nil, types.ErrOperationInactive
	}

	// 2. Validate batch mode allows immediate
	if reg.BatchMode == types.ShieldBatchMode_SHIELD_BATCH_MODE_ENCRYPTED_ONLY {
		return nil, types.ErrImmediateNotAllowed
	}

	// 3. Validate proof domain matches registration
	if msg.ProofDomain != reg.ProofDomain {
		return nil, types.ErrProofDomainMismatch
	}

	// 4. Validate minimum trust level meets requirement
	if msg.MinTrustLevel < reg.MinTrustLevel {
		return nil, types.ErrInsufficientTrustLevel
	}

	// 5. Resolve nullifier scope and verify ZK proof
	scope := k.resolveNullifierScope(ctx, reg, msg)
	if err := k.verifyProof(ctx, msg, scope); err != nil {
		return nil, err
	}

	// 6. Check and record nullifier
	nullifierHex := hex.EncodeToString(msg.Nullifier)
	if k.IsNullifierUsed(ctx, reg.NullifierDomain, scope, nullifierHex) {
		return nil, types.ErrNullifierUsed
	}
	if err := k.RecordNullifier(ctx, reg.NullifierDomain, scope, nullifierHex, ctx.BlockHeight()); err != nil {
		return nil, err
	}

	// 7. Check per-identity rate limit
	rateLimitHex := hex.EncodeToString(msg.RateLimitNullifier)
	if !k.CheckAndIncrementRateLimit(ctx, rateLimitHex, params.MaxExecsPerIdentityPerEpoch) {
		return nil, types.ErrRateLimitExceeded
	}

	// 8. Decode, validate signer, and execute inner message
	resp, err := k.executeInnerMessage(ctx, params, msg.InnerMessage)
	if err != nil {
		return nil, err
	}

	// 9. Emit event
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeShieldedExec,
		sdk.NewAttribute(types.AttributeKeyMessageType, typeURL),
		sdk.NewAttribute(types.AttributeKeyNullifierDomain, fmt.Sprintf("%d", reg.NullifierDomain)),
		sdk.NewAttribute(types.AttributeKeyNullifierHex, nullifierHex),
		sdk.NewAttribute(types.AttributeKeyExecMode, "immediate"),
	))

	return &types.MsgShieldedExecResponse{InnerResponse: resp}, nil
}

// handleEncryptedBatch validates cleartext fields and queues the encrypted payload.
func (k msgServer) handleEncryptedBatch(ctx sdk.Context, params types.Params, msg *types.MsgShieldedExec) (*types.MsgShieldedExecResponse, error) {
	if !params.EncryptedBatchEnabled {
		return nil, types.ErrEncryptedBatchDisabled
	}

	// 1. Reject cleartext fields in encrypted batch mode
	if msg.InnerMessage != nil {
		return nil, types.ErrCleartextFieldInBatchMode
	}
	if len(msg.Proof) > 0 {
		return nil, types.ErrCleartextFieldInBatchMode
	}

	// 2. Validate encrypted payload
	if len(msg.EncryptedPayload) == 0 {
		return nil, types.ErrMissingEncryptedPayload
	}
	if uint32(len(msg.EncryptedPayload)) > params.MaxEncryptedPayloadSize {
		return nil, types.ErrPayloadTooLarge
	}

	// 3. Validate target epoch is current
	epochState, found := k.GetShieldEpochStateVal(ctx)
	if !found {
		return nil, types.ErrEncryptedBatchDisabled
	}
	if msg.TargetEpoch != epochState.CurrentEpoch {
		return nil, types.ErrInvalidTargetEpoch
	}

	// 4. Check pending queue capacity
	pendingCount := k.GetPendingOpCountVal(ctx)
	if pendingCount >= uint64(params.MaxPendingQueueSize) {
		return nil, types.ErrPendingQueueFull
	}

	// 5. Validate merkle root
	if err := k.validateMerkleRoot(ctx, msg.MerkleRoot, msg.ProofDomain); err != nil {
		return nil, err
	}

	// 6. Check nullifier not already pending
	nullifierHex := hex.EncodeToString(msg.Nullifier)
	if k.IsPendingNullifier(ctx, nullifierHex) {
		return nil, types.ErrNullifierUsed
	}
	if err := k.RecordPendingNullifier(ctx, nullifierHex); err != nil {
		return nil, err
	}

	// 7. Check per-identity rate limit
	rateLimitHex := hex.EncodeToString(msg.RateLimitNullifier)
	if !k.CheckAndIncrementRateLimit(ctx, rateLimitHex, params.MaxExecsPerIdentityPerEpoch) {
		return nil, types.ErrRateLimitExceeded
	}

	// 8. Store pending operation
	opID := k.GetNextPendingOpID(ctx)
	if err := k.SetPendingOp(ctx, types.PendingShieldedOp{
		Id:                opID,
		TargetEpoch:       msg.TargetEpoch,
		Nullifier:         msg.Nullifier,
		MerkleRoot:        msg.MerkleRoot,
		ProofDomain:       msg.ProofDomain,
		MinTrustLevel:     msg.MinTrustLevel,
		EncryptedPayload:  msg.EncryptedPayload,
		SubmittedAtHeight: ctx.BlockHeight(),
		SubmittedAtEpoch:  epochState.CurrentEpoch,
	}); err != nil {
		return nil, err
	}

	// 9. Emit queued event
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
func (k Keeper) resolveNullifierScope(ctx context.Context, reg types.ShieldedOpRegistration, msg *types.MsgShieldedExec) uint64 {
	switch reg.NullifierScopeType {
	case types.NullifierScopeType_NULLIFIER_SCOPE_EPOCH:
		return k.GetCurrentEpoch(ctx)
	case types.NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD:
		if msg.InnerMessage != nil && reg.ScopeFieldPath != "" {
			if val, ok := extractUint64Field(k.cdc, msg.InnerMessage, reg.ScopeFieldPath); ok {
				return val
			}
		}
		// Fallback to epoch scope if field extraction fails
		return k.GetCurrentEpoch(ctx)
	case types.NullifierScopeType_NULLIFIER_SCOPE_GLOBAL:
		return 0
	default:
		return k.GetCurrentEpoch(ctx)
	}
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

	// Verify inner message signer is the shield module account.
	// This ensures the inner message was crafted to be executed through the shield,
	// not a regular message that happened to have the same type URL.
	moduleAddr := k.accountKeeper.GetModuleAddress(types.ModuleName)
	if legacyMsg, ok := innerMsg.(sdk.LegacyMsg); ok {
		signers := legacyMsg.GetSigners()
		if len(signers) == 0 || !signers[0].Equals(moduleAddr) {
			return nil, types.ErrInvalidInnerMessageSigner
		}
	} else if pc, ok := k.cdc.(*codec.ProtoCodec); ok {
		signerBytes, _, err := pc.GetMsgV1Signers(innerMsg)
		if err != nil || len(signerBytes) == 0 || !sdk.AccAddress(signerBytes[0]).Equals(moduleAddr) {
			return nil, types.ErrInvalidInnerMessageSigner
		}
	}
	// If neither check can be performed (no LegacyMsg, no ProtoCodec),
	// we rely on the registered operation whitelist for security.

	// Check ShieldAware gate: target module must explicitly opt in.
	// This is the second gate beyond the governance whitelist.
	if sa, found := k.getShieldAware(innerMsgAny.TypeUrl); found {
		if !sa.IsShieldCompatible(ctx, innerMsg) {
			return nil, types.ErrIncompatibleOperation
		}
	}
	// If no ShieldAware implementation is registered, we allow execution.
	// The governance whitelist (registered operation) is the primary gate.

	// Dispatch via router
	handler := k.late.router.Handler(innerMsg)
	if handler == nil {
		return nil, types.ErrIncompatibleOperation
	}

	// Execute with gas limit
	childCtx := ctx.WithGasMeter(storetypes.NewGasMeter(params.MaxGasPerExec))
	result, err := handler(childCtx, innerMsg)
	if err != nil {
		return nil, err
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
