package keeper

import (
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	commontypes "sparkdream/x/common/types"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/shield/types"
)

// EndBlocker handles epoch advancement and batch execution.
//
// The epoch advances whether or not encrypted batch mode is enabled: immediate
// mode reads it for EPOCH-scoped nullifiers and per-epoch rate limits, so a
// frozen epoch would turn those into lifetime limits. Pending ops expire
// whether or not batch mode is enabled, so disabling it (governance, a re-key)
// drains the queue instead of freezing it.
func (k Keeper) EndBlocker(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil
	}

	epochState, found := k.GetShieldEpochStateVal(ctx)
	if !found {
		// Initialize epoch state on first run
		if err := k.SetShieldEpochStateVal(ctx, types.ShieldEpochState{
			CurrentEpoch:     0,
			EpochStartHeight: sdkCtx.BlockHeight(),
		}); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("EndBlocker: k.SetShieldEpochStateVal failed", "err", err)
		}
		return nil
	}

	currentHeight := sdkCtx.BlockHeight()
	currentEpoch := epochState.CurrentEpoch

	// Advance the epoch at the boundary, batch mode or not: immediate mode
	// reads it for EPOCH-scoped nullifiers and per-epoch rate limits.
	atBoundary := currentHeight >= epochState.EpochStartHeight+commontypes.Saturate[int64](params.ShieldEpochInterval)
	if atBoundary {
		currentEpoch++
		if err := k.SetShieldEpochStateVal(ctx, types.ShieldEpochState{
			CurrentEpoch:     currentEpoch,
			EpochStartHeight: currentHeight,
		}); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("EndBlocker: k.SetShieldEpochStateVal failed", "err", err)
		}
	}

	if params.EncryptedBatchEnabled || params.BeaconEnabled {
		// Liveness first, every block: a demand is judged exactly one epoch
		// interval after it was recorded, before expiry can forget it (a
		// force-demanded lone op's epoch falls out of the window at the very
		// boundary it would be judged at). Beacon demands are judged like
		// batch demands: a validator that stops serving the beacon is as
		// absent as one that stops serving batches.
		k.judgeTLELiveness(ctx, params, currentHeight)
	}
	k.expirePendingOps(ctx, params, currentEpoch)
	if atBoundary {
		// The beacon: the epoch that just closed is demanded at once (see
		// beacon.go), whether or not anything was submitted to it.
		k.recordBeaconDemand(ctx, params, currentEpoch-1, currentHeight)
	}
	if params.EncryptedBatchEnabled {
		// Every block, not just at boundaries: a closed epoch's key arrives
		// whenever enough validators' shares land, and the batch should run
		// in that block rather than wait for the next boundary.
		k.recordDecryptionDemands(ctx, params, currentEpoch, currentHeight)
		k.processReadyBatches(ctx, params, currentEpoch)
	}

	if atBoundary {
		k.pruneStaleState(ctx, currentEpoch)
	}

	return nil
}

// pendingCutoff is the oldest epoch whose ops are still allowed to wait.
func pendingCutoff(params types.Params, currentEpoch uint64) uint64 {
	if currentEpoch > uint64(params.MaxPendingEpochs) {
		return currentEpoch - uint64(params.MaxPendingEpochs)
	}
	return 0
}

// pendingByEpoch counts pending ops per closed epoch (target epoch strictly
// before currentEpoch), walking keys only.
func (k Keeper) pendingByEpoch(ctx context.Context, currentEpoch uint64) map[uint64]int {
	return k.CountPendingOpsBeforeEpoch(ctx, currentEpoch)
}

// recordDecryptionDemands decides, per closed epoch, whether its batch is
// ready to be decrypted, and records the demand validators act on. Shares
// (and therefore the epoch key) are only released once a batch is ready, so
// a small batch keeps waiting encrypted rather than sitting decryptable on
// chain while it waits for company.
//
// An epoch is ready when it alone meets min_batch_size, when every closed
// epoch together does (they execute in the same block and form one
// anonymity set), or when it has waited max_pending_epochs.
func (k Keeper) recordDecryptionDemands(ctx context.Context, params types.Params, currentEpoch uint64, currentHeight int64) {
	counts := k.pendingByEpoch(ctx, currentEpoch)
	if len(counts) == 0 {
		return
	}
	total := 0
	for _, c := range counts {
		total += c
	}
	for epoch, c := range counts {
		if _, demanded := k.GetDecryptionDemand(ctx, epoch); demanded {
			continue
		}
		ready := c >= int(params.MinBatchSize) ||
			total >= int(params.MinBatchSize) ||
			currentEpoch-epoch >= uint64(params.MaxPendingEpochs)
		if ready {
			if err := k.SetDecryptionDemand(ctx, epoch, currentHeight); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("recordDecryptionDemands: k.SetDecryptionDemand failed", "err", err)
			}
		}
	}
}

// processReadyBatches executes every closed epoch whose key has been
// reconstructed (or seeded) and whose batch was demanded, oldest first.
// max_ops_per_batch is one budget for the whole EndBlocker, shared across
// epochs: when several epochs become ready in the same block (keys that
// arrive late), the ops past the budget stay pending for the next block.
func (k Keeper) processReadyBatches(ctx context.Context, params types.Params, currentEpoch uint64) {
	counts := k.pendingByEpoch(ctx, currentEpoch)
	if len(counts) == 0 {
		return
	}
	epochs := make([]uint64, 0, len(counts))
	for epoch := range counts {
		epochs = append(epochs, epoch)
	}
	sort.Slice(epochs, func(i, j int) bool { return epochs[i] < epochs[j] })
	budget := int(params.MaxOpsPerBatch)
	for _, epoch := range epochs {
		if budget <= 0 {
			return
		}
		if _, demanded := k.GetDecryptionDemand(ctx, epoch); !demanded {
			continue
		}
		decKey, found := k.GetShieldEpochDecryptionKeyVal(ctx, epoch)
		if !found {
			continue
		}
		budget -= k.executeBatch(ctx, params, epoch, decKey, budget)
	}
}

// executeBatch decrypts, verifies, shuffles and executes at most budget of
// one epoch's pending ops (the rest run next block) and returns how many it
// took off the queue.
func (k Keeper) executeBatch(ctx context.Context, params types.Params, epoch uint64, decKey types.ShieldEpochDecryptionKey, budget int) int {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	pendingOps := k.GetPendingOpsForEpoch(ctx, epoch)
	if len(pendingOps) == 0 || budget <= 0 {
		return 0
	}
	if len(decKey.DecryptionKey) == 0 {
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeShieldBatchSkipped,
			sdk.NewAttribute(types.AttributeKeyEpoch, fmt.Sprintf("%d", epoch)),
			sdk.NewAttribute(types.AttributeKeyError, "empty decryption key"),
		))
		return 0
	}
	if len(pendingOps) > budget {
		pendingOps = pendingOps[:budget]
	}

	// Deterministic shuffle for unlinkability
	seed := makeShuffleSeed(sdkCtx.BlockHeader().AppHash, epoch)
	pendingOps = deterministicShuffle(pendingOps, seed)

	executed := 0
	decryptFailed := 0
	proofFailed := 0
	otherFailed := 0
	for _, op := range pendingOps {
		result := k.processEncryptedOp(ctx, params, &op, decKey.DecryptionKey)
		switch result {
		case batchResultOK:
			executed++
		case batchResultDecryptFail:
			decryptFailed++
		case batchResultProofFail:
			proofFailed++
		default:
			otherFailed++
		}
		// Clean up pending op and its submission nullifier regardless of outcome
		if err := k.DeletePendingOp(ctx, op.TargetEpoch, op.Id); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("executeBatch: k.DeletePendingOp failed", "err", err)
		}
		if err := k.DeletePendingNullifier(ctx, hex.EncodeToString(op.SubmissionNullifier)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("executeBatch: k.DeletePendingNullifier failed", "err", err)
		}
	}

	dropped := decryptFailed + proofFailed + otherFailed
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeShieldBatchExecuted,
		sdk.NewAttribute(types.AttributeKeyEpoch, fmt.Sprintf("%d", epoch)),
		sdk.NewAttribute(types.AttributeKeyBatchSize, fmt.Sprintf("%d", len(pendingOps))),
		sdk.NewAttribute(types.AttributeKeyExecuted, fmt.Sprintf("%d", executed)),
		sdk.NewAttribute(types.AttributeKeyDropped, fmt.Sprintf("%d", dropped)),
		sdk.NewAttribute(types.AttributeKeyDecryptFailed, fmt.Sprintf("%d", decryptFailed)),
		sdk.NewAttribute(types.AttributeKeyProofFailed, fmt.Sprintf("%d", proofFailed)),
	))
	return len(pendingOps)
}

// expirePendingOps drops ops that waited longer than max_pending_epochs
// without their epoch's key, and forgets those epochs' demands.
func (k Keeper) expirePendingOps(ctx context.Context, params types.Params, currentEpoch uint64) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	cutoffEpoch := pendingCutoff(params, currentEpoch)
	if cutoffEpoch == 0 {
		return
	}

	expiredOps := k.GetPendingOpsBeforeEpoch(ctx, cutoffEpoch)
	for _, op := range expiredOps {
		if err := k.DeletePendingOp(ctx, op.TargetEpoch, op.Id); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("expirePendingOps: k.DeletePendingOp failed", "err", err)
		}
		if err := k.DeletePendingNullifier(ctx, hex.EncodeToString(op.SubmissionNullifier)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("expirePendingOps: k.DeletePendingNullifier failed", "err", err)
		}
	}
	var stale []uint64
	if err := k.IterateDecryptionDemands(ctx, func(epoch uint64, _ int64) bool {
		if epoch < cutoffEpoch {
			stale = append(stale, epoch)
			return false
		}
		return true
	}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("expirePendingOps: k.IterateDecryptionDemands failed", "err", err)
	}
	for _, epoch := range stale {
		if err := k.RemoveDecryptionDemand(ctx, epoch); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("expirePendingOps: k.RemoveDecryptionDemand failed", "err", err)
		}
	}

	if len(expiredOps) > 0 {
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeShieldBatchExpired,
			sdk.NewAttribute(types.AttributeKeyCount, fmt.Sprintf("%d", len(expiredOps))),
			sdk.NewAttribute(types.AttributeKeyCutoffEpoch, fmt.Sprintf("%d", cutoffEpoch)),
		))
	}
}

// Batch processing result codes for diagnostic tracking.
const (
	batchResultOK          = 0
	batchResultDecryptFail = 1
	batchResultProofFail   = 2
	batchResultOtherFail   = 3
)

// processEncryptedOp decrypts, verifies, and executes a single pending shielded op.
// Returns a result code for diagnostic tracking.
//
// It runs in the EndBlocker, where no runTx recovery exists: a panic here
// (the inner handler running out of its max_gas_per_exec meter, or any
// downstream bug) would propagate out of FinalizeBlock and halt every node at
// the same height. Every panic is therefore recovered into a failed op, and
// the inner message runs on a cache context written back only on success, so
// a handler that errors or panics part-way leaves no partial state.
func (k Keeper) processEncryptedOp(ctx context.Context, params types.Params, op *types.PendingShieldedOp, decryptionKey []byte) (result int) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	defer func() {
		if r := recover(); r != nil {
			sdkCtx.Logger().With("module", "x/"+types.ModuleName).Error(
				"shielded batch op panicked; dropped", "pending_op_id", op.Id, "panic", fmt.Sprintf("%v", r))
			result = batchResultOtherFail
		}
	}()

	// 1. Decrypt the payload
	plaintext, err := decryptPayload(op.EncryptedPayload, decryptionKey, op.TargetEpoch)
	if err != nil {
		return batchResultDecryptFail
	}

	// 2. Parse the decrypted inner message as a proto-encoded MsgShieldedExec fragment.
	if len(plaintext) < 4 {
		return batchResultDecryptFail
	}

	var innerExec types.MsgShieldedExec
	if err := k.cdc.Unmarshal(plaintext, &innerExec); err != nil {
		return batchResultDecryptFail
	}

	// 3. The fragment carries the inner message, the operation proof and the
	// operation nullifier; restore the rest from the pending op. The
	// operation proof's public inputs must reproduce every one of them,
	// including the rate-limit nullifier the submission proof established
	// (so the operation was made by the member who paid for the submission,
	// in the epoch it was submitted).
	if len(innerExec.Nullifier) != 32 {
		return batchResultOtherFail
	}
	innerExec.MerkleRoot = op.MerkleRoot
	innerExec.ProofDomain = op.ProofDomain
	innerExec.MinTrustLevel = op.MinTrustLevel
	innerExec.RateLimitNullifier = op.RateLimitNullifier
	// The sealed operation nullifier must be canonical like every cleartext
	// one (the rate-limit nullifier and root were checked at submission).
	if err := types.ValidateCanonicalPublicInputs(innerExec.Nullifier, innerExec.RateLimitNullifier, innerExec.MerkleRoot); err != nil {
		return batchResultOtherFail
	}

	if innerExec.InnerMessage == nil {
		return batchResultOtherFail
	}
	typeURL := innerExec.InnerMessage.TypeUrl
	reg, regFound := k.GetShieldedOp(ctx, typeURL)
	if !regFound || !reg.Active {
		return batchResultOtherFail
	}

	// Validate batch mode allows encrypted batch execution.
	// Operations registered as IMMEDIATE_ONLY must not execute in batch mode.
	if reg.BatchMode == types.ShieldBatchMode_SHIELD_BATCH_MODE_IMMEDIATE_ONLY {
		return batchResultOtherFail
	}

	// The submission was proven in the op's domain; the operation must be
	// registered for that domain (a trust-tree proof must never execute a
	// group-tree operation such as a secret-ballot vote).
	if op.ProofDomain != reg.ProofDomain {
		return batchResultOtherFail
	}

	// Validate minimum trust level meets registration requirement.
	if op.MinTrustLevel < reg.MinTrustLevel {
		return batchResultOtherFail
	}

	// Ownership proofs bind the owner's sequence at execution, which a batch
	// submitter can't predict; they are immediate-only.
	if reg.NullifierMode == types.NullifierMode_NULLIFIER_MODE_OWNERSHIP {
		return batchResultOtherFail
	}

	// 4. Resolve scope and verify ZK proof against the epoch the op was
	// submitted in: the prover bound RateLimitEpoch (and any EPOCH scope)
	// to that epoch, and the EndBlocker has already advanced the epoch by
	// the time a batch runs. The merkle root was validated at submission
	// and is not re-checked against the live tree ("valid when
	// submitted"; the tree may have rotated several times since).
	scope, err := k.resolveNullifierScopeAtEpoch(ctx, reg, &innerExec, op.SubmittedAtEpoch)
	if err != nil {
		return batchResultOtherFail
	}
	if err := k.verifyProofAtEpoch(ctx, &innerExec, consumeBinding(reg.NullifierDomain, scope, &innerExec), op.SubmittedAtEpoch, false); err != nil {
		return batchResultProofFail
	}

	// 5. Check and record the operation nullifier in the permanent store
	nullifierHex := hex.EncodeToString(innerExec.Nullifier)
	if k.IsNullifierUsed(ctx, reg.NullifierDomain, scope, nullifierHex) {
		return batchResultOtherFail
	}
	if err := k.RecordNullifier(ctx, reg.NullifierDomain, scope, nullifierHex, sdkCtx.BlockHeight()); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("processEncryptedOp: k.RecordNullifier failed", "err", err)
	}

	// 6. Execute the inner message on a cache context. The nullifier above
	// stays recorded on failure: the op was proven and consumed.
	cacheCtx, writeCache := sdkCtx.CacheContext()
	execCtx := types.WithExecNullifier(types.WithProvenTrustLevel(cacheCtx, op.MinTrustLevel), types.ExecNullifier{
		Domain:    reg.NullifierDomain,
		Scope:     scope,
		Nullifier: innerExec.Nullifier,
	})
	_, err = k.executeInnerMessage(execCtx, params, innerExec.InnerMessage)
	if err != nil {
		return batchResultOtherFail
	}
	writeCache()

	return batchResultOK
}
