package keeper

import (
	"context"
	"encoding/hex"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/shield/types"
)

// EndBlocker handles epoch advancement and batch execution.
func (k Keeper) EndBlocker(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil
	}

	if !params.EncryptedBatchEnabled {
		return nil
	}

	epochState, found := k.GetShieldEpochStateVal(ctx)
	if !found {
		// Initialize epoch state on first run
		_ = k.SetShieldEpochStateVal(ctx, types.ShieldEpochState{
			CurrentEpoch:     0,
			EpochStartHeight: sdkCtx.BlockHeight(),
		})
		return nil
	}

	currentHeight := sdkCtx.BlockHeight()

	// Check if we've crossed an epoch boundary
	if currentHeight < epochState.EpochStartHeight+int64(params.ShieldEpochInterval) {
		return nil
	}

	// Advance to next epoch
	newEpoch := epochState.CurrentEpoch + 1
	_ = k.SetShieldEpochStateVal(ctx, types.ShieldEpochState{
		CurrentEpoch:     newEpoch,
		EpochStartHeight: currentHeight,
	})

	// Try to process the PREVIOUS epoch's pending ops
	prevEpoch := epochState.CurrentEpoch
	k.tryProcessBatch(ctx, params, prevEpoch, currentHeight)

	// Also try to process any carried-over ops from older epochs
	k.processCarriedOverBatches(ctx, params, prevEpoch, currentHeight)

	// Check TLE liveness — increment miss counters and jail violators
	k.checkTLELiveness(ctx, prevEpoch)

	// Prune stale state
	k.pruneStaleState(ctx, newEpoch)

	return nil
}

// tryProcessBatch attempts to decrypt and execute pending ops for a given epoch.
func (k Keeper) tryProcessBatch(ctx context.Context, params types.Params, epoch uint64, currentHeight int64) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get decryption key for this epoch
	_, found := k.GetShieldEpochDecryptionKeyVal(ctx, epoch)
	if !found {
		return // validators haven't produced the key yet; ops carry over
	}

	// Collect all pending ops for this epoch
	pendingOps := k.GetPendingOpsForEpoch(ctx, epoch)
	if len(pendingOps) == 0 {
		return
	}

	// Check min_batch_size (unless max_pending_epochs forces execution)
	oldestSubmittedEpoch := k.getOldestPendingEpoch(pendingOps)
	epochsWaiting := epoch - oldestSubmittedEpoch
	forceExecute := epochsWaiting >= uint64(params.MaxPendingEpochs)

	if len(pendingOps) < int(params.MinBatchSize) && !forceExecute {
		return // batch too small, carry over
	}

	// Limit ops processed per block
	if uint32(len(pendingOps)) > params.MaxOpsPerBatch {
		pendingOps = pendingOps[:params.MaxOpsPerBatch]
	}

	// Decrypt, verify, shuffle, and execute
	decKey, _ := k.GetShieldEpochDecryptionKeyVal(ctx, epoch)

	// Validate decryption key is non-empty
	if len(decKey.DecryptionKey) == 0 {
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeShieldBatchSkipped,
			sdk.NewAttribute(types.AttributeKeyEpoch, fmt.Sprintf("%d", epoch)),
			sdk.NewAttribute(types.AttributeKeyError, "empty decryption key"),
		))
		return
	}

	// Deterministic shuffle for unlinkability
	seed := makeShuffleSeed(sdkCtx.BlockHeader().AppHash, epoch)
	pendingOps = deterministicShuffle(pendingOps, seed)

	executed := 0
	decryptFailed := 0
	proofFailed := 0
	otherFailed := 0
	for _, op := range pendingOps {
		// Attempt to decrypt and execute each op
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
		// Clean up pending op and its nullifier regardless of outcome
		_ = k.DeletePendingOp(ctx, op.Id)
		_ = k.DeletePendingNullifier(ctx, hex.EncodeToString(op.Nullifier))
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
}

// processCarriedOverBatches handles ops from older epochs and expires stale ones.
func (k Keeper) processCarriedOverBatches(ctx context.Context, params types.Params, currentEpoch uint64, currentHeight int64) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Guard against underflow on early chain startup
	var cutoffEpoch uint64
	if currentEpoch > uint64(params.MaxPendingEpochs) {
		cutoffEpoch = currentEpoch - uint64(params.MaxPendingEpochs)
	}

	// 1. Try to process older epochs that now have late-arrived decryption keys
	for epoch := cutoffEpoch; epoch < currentEpoch; epoch++ {
		if _, found := k.GetShieldEpochDecryptionKeyVal(ctx, epoch); found {
			pendingOps := k.GetPendingOpsForEpoch(ctx, epoch)
			if len(pendingOps) > 0 {
				k.tryProcessBatch(ctx, params, epoch, currentHeight)
			}
		}
	}

	// 2. Expire ops from epochs older than max_pending_epochs
	expiredOps := k.GetPendingOpsBeforeEpoch(ctx, cutoffEpoch)
	for _, op := range expiredOps {
		_ = k.DeletePendingOp(ctx, op.Id)
		_ = k.DeletePendingNullifier(ctx, hex.EncodeToString(op.Nullifier))
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
func (k Keeper) processEncryptedOp(ctx context.Context, params types.Params, op *types.PendingShieldedOp, decryptionKey []byte) int {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// 1. Decrypt the payload
	plaintext, err := decryptPayload(op.EncryptedPayload, decryptionKey)
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

	// 3. Validate inner message and look up registration
	innerExec.MerkleRoot = op.MerkleRoot
	innerExec.ProofDomain = op.ProofDomain
	innerExec.MinTrustLevel = op.MinTrustLevel
	innerExec.Nullifier = op.Nullifier

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

	// Validate minimum trust level meets registration requirement.
	if op.MinTrustLevel < reg.MinTrustLevel {
		return batchResultOtherFail
	}

	// 4. Resolve scope and verify ZK proof
	scope := k.resolveNullifierScope(ctx, reg, &innerExec)
	if err := k.verifyProof(ctx, &innerExec, scope); err != nil {
		return batchResultProofFail
	}

	// 5. Check and record nullifier in the permanent store
	nullifierHex := hex.EncodeToString(op.Nullifier)
	if k.IsNullifierUsed(ctx, reg.NullifierDomain, scope, nullifierHex) {
		return batchResultOtherFail
	}
	_ = k.RecordNullifier(ctx, reg.NullifierDomain, scope, nullifierHex, sdkCtx.BlockHeight())

	// 6. Execute the inner message
	_, err = k.executeInnerMessage(sdkCtx, params, innerExec.InnerMessage)
	if err != nil {
		return batchResultOtherFail
	}

	return batchResultOK
}

// getOldestPendingEpoch returns the earliest submitted epoch among pending ops.
func (k Keeper) getOldestPendingEpoch(ops []types.PendingShieldedOp) uint64 {
	if len(ops) == 0 {
		return 0
	}
	oldest := ops[0].SubmittedAtEpoch
	for _, op := range ops[1:] {
		if op.SubmittedAtEpoch < oldest {
			oldest = op.SubmittedAtEpoch
		}
	}
	return oldest
}
