package keeper

import (
	"context"

	"sparkdream/x/shield/types"
)

// GetPendingOpCountVal returns the total number of pending operations.
// Reads from the PendingOpCount collections.Item instead of iterating all PendingOps.
func (k Keeper) GetPendingOpCountVal(ctx context.Context) uint64 {
	count, err := k.PendingOpCount.Get(ctx)
	if err != nil {
		return 0
	}
	return count
}

// GetNextPendingOpID returns and increments the next pending op ID.
func (k Keeper) GetNextPendingOpID(ctx context.Context) uint64 {
	id, err := k.NextPendingOpId.Next(ctx)
	if err != nil {
		return 0
	}
	return id
}

// SetPendingOp stores a pending shielded operation and increments the counter.
func (k Keeper) SetPendingOp(ctx context.Context, op types.PendingShieldedOp) error {
	// Check if this is a new op or an overwrite
	_, err := k.PendingOps.Get(ctx, op.Id)
	isNew := err != nil

	if err := k.PendingOps.Set(ctx, op.Id, op); err != nil {
		return err
	}
	// Only increment count for new ops, not overwrites
	if isNew {
		count := k.GetPendingOpCountVal(ctx)
		return k.PendingOpCount.Set(ctx, count+1)
	}
	return nil
}

// DeletePendingOp removes a pending operation and decrements the counter.
func (k Keeper) DeletePendingOp(ctx context.Context, id uint64) error {
	if err := k.PendingOps.Remove(ctx, id); err != nil {
		return err
	}
	count := k.GetPendingOpCountVal(ctx)
	if count > 0 {
		return k.PendingOpCount.Set(ctx, count-1)
	}
	return nil
}

// GetPendingOpsForEpoch returns all pending ops targeting a specific epoch.
func (k Keeper) GetPendingOpsForEpoch(ctx context.Context, epoch uint64) []types.PendingShieldedOp {
	var ops []types.PendingShieldedOp
	iter, err := k.PendingOps.Iterate(ctx, nil)
	if err != nil {
		return nil
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		val, err := iter.Value()
		if err != nil {
			continue
		}
		if val.TargetEpoch == epoch {
			ops = append(ops, val)
		}
	}
	return ops
}

// GetPendingOpsBeforeEpoch returns all pending ops from epochs before cutoffEpoch.
func (k Keeper) GetPendingOpsBeforeEpoch(ctx context.Context, cutoffEpoch uint64) []types.PendingShieldedOp {
	var ops []types.PendingShieldedOp
	iter, err := k.PendingOps.Iterate(ctx, nil)
	if err != nil {
		return nil
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		val, err := iter.Value()
		if err != nil {
			continue
		}
		if val.SubmittedAtEpoch < cutoffEpoch {
			ops = append(ops, val)
		}
	}
	return ops
}

// IteratePendingOps iterates over all pending operations.
func (k Keeper) IteratePendingOps(ctx context.Context, fn func(types.PendingShieldedOp) bool) error {
	iter, err := k.PendingOps.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		val, err := iter.Value()
		if err != nil {
			return err
		}
		if fn(val) {
			break
		}
	}
	return nil
}
