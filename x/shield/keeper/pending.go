package keeper

import (
	"context"

	"cosmossdk.io/collections"

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

func pendingOpKey(op types.PendingShieldedOp) collections.Pair[uint64, uint64] {
	return collections.Join(op.TargetEpoch, op.Id)
}

// SetPendingOp stores a pending shielded operation and increments the counter.
func (k Keeper) SetPendingOp(ctx context.Context, op types.PendingShieldedOp) error {
	key := pendingOpKey(op)
	exists, err := k.PendingOps.Has(ctx, key)
	if err != nil {
		return err
	}
	if err := k.PendingOps.Set(ctx, key, op); err != nil {
		return err
	}
	// Only increment count for new ops, not overwrites
	if !exists {
		return k.PendingOpCount.Set(ctx, k.GetPendingOpCountVal(ctx)+1)
	}
	return nil
}

// DeletePendingOp removes a pending operation and decrements the counter.
// Deleting an op that is not there changes nothing.
func (k Keeper) DeletePendingOp(ctx context.Context, targetEpoch, id uint64) error {
	key := collections.Join(targetEpoch, id)
	exists, err := k.PendingOps.Has(ctx, key)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if err := k.PendingOps.Remove(ctx, key); err != nil {
		return err
	}
	if count := k.GetPendingOpCountVal(ctx); count > 0 {
		return k.PendingOpCount.Set(ctx, count-1)
	}
	return nil
}

// GetPendingOpsForEpoch returns all pending ops targeting a specific epoch
// (one prefix range, in id order).
func (k Keeper) GetPendingOpsForEpoch(ctx context.Context, epoch uint64) []types.PendingShieldedOp {
	iter, err := k.PendingOps.Iterate(ctx, collections.NewPrefixedPairRange[uint64, uint64](epoch))
	if err != nil {
		return nil
	}
	defer iter.Close()
	ops, err := iter.Values()
	if err != nil {
		return nil
	}
	return ops
}

// GetPendingOpsBeforeEpoch returns all pending ops whose target epoch is
// before cutoffEpoch (target epoch and submission epoch are always equal).
func (k Keeper) GetPendingOpsBeforeEpoch(ctx context.Context, cutoffEpoch uint64) []types.PendingShieldedOp {
	rng := new(collections.Range[collections.Pair[uint64, uint64]]).
		EndExclusive(collections.Join(cutoffEpoch, uint64(0)))
	iter, err := k.PendingOps.Iterate(ctx, rng)
	if err != nil {
		return nil
	}
	defer iter.Close()
	ops, err := iter.Values()
	if err != nil {
		return nil
	}
	return ops
}

// CountPendingOpsBeforeEpoch counts pending ops per target epoch strictly
// before epoch. It walks keys only: the payloads (up to
// max_encrypted_payload_size each) are never decoded.
func (k Keeper) CountPendingOpsBeforeEpoch(ctx context.Context, epoch uint64) map[uint64]int {
	counts := map[uint64]int{}
	if k.GetPendingOpCountVal(ctx) == 0 {
		return counts
	}
	rng := new(collections.Range[collections.Pair[uint64, uint64]]).
		EndExclusive(collections.Join(epoch, uint64(0)))
	iter, err := k.PendingOps.Iterate(ctx, rng)
	if err != nil {
		return counts
	}
	defer iter.Close()
	keys, err := iter.Keys()
	if err != nil {
		return counts
	}
	for _, key := range keys {
		counts[key.K1()]++
	}
	return counts
}

// IteratePendingOps iterates over all pending operations in (epoch, id) order.
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
