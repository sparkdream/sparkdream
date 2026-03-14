package keeper

import (
	"context"

	"cosmossdk.io/collections"
)

// CheckAndIncrementRateLimit checks the per-identity rate limit and increments the counter.
// Returns true if the operation is within the rate limit, false if exceeded.
func (k Keeper) CheckAndIncrementRateLimit(ctx context.Context, rateLimitNullifierHex string, maxPerEpoch uint64) bool {
	epoch := k.GetCurrentEpoch(ctx)
	key := collections.Join(epoch, rateLimitNullifierHex)

	count, err := k.IdentityRateLimits.Get(ctx, key)
	if err != nil {
		count = 0
	}

	if count >= maxPerEpoch {
		return false
	}

	_ = k.IdentityRateLimits.Set(ctx, key, count+1)
	return true
}

// GetIdentityRateLimitCount returns the current rate limit count for a given identity in the current epoch.
func (k Keeper) GetIdentityRateLimitCount(ctx context.Context, rateLimitNullifierHex string) uint64 {
	epoch := k.GetCurrentEpoch(ctx)
	key := collections.Join(epoch, rateLimitNullifierHex)
	count, err := k.IdentityRateLimits.Get(ctx, key)
	if err != nil {
		return 0
	}
	return count
}

// PruneIdentityRateLimits removes rate limit entries for epochs before cutoffEpoch.
func (k Keeper) PruneIdentityRateLimits(ctx context.Context, cutoffEpoch uint64) error {
	iter, err := k.IdentityRateLimits.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	defer iter.Close()

	var toDelete []collections.Pair[uint64, string]
	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return err
		}
		epoch := key.K1()
		if epoch < cutoffEpoch {
			toDelete = append(toDelete, key)
		}
	}

	for _, key := range toDelete {
		if err := k.IdentityRateLimits.Remove(ctx, key); err != nil {
			return err
		}
	}
	return nil
}
