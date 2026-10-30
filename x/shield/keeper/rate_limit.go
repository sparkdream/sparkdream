package keeper

import (
	"context"
	sdk "github.com/cosmos/cosmos-sdk/types"

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

	if err := k.IdentityRateLimits.Set(ctx, key, count+1); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("CheckAndIncrementRateLimit: IdentityRateLimits.Set failed", "err", err)
	}
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

// PruneIdentityRateLimits removes rate limit entries for epochs before
// cutoffEpoch. Keys are (epoch, identity), so the stale entries are exactly
// the range below (cutoffEpoch, "") and nothing newer is visited.
func (k Keeper) PruneIdentityRateLimits(ctx context.Context, cutoffEpoch uint64) error {
	rng := new(collections.Range[collections.Pair[uint64, string]]).
		EndExclusive(collections.Join(cutoffEpoch, ""))
	iter, err := k.IdentityRateLimits.Iterate(ctx, rng)
	if err != nil {
		return err
	}
	// Collect first: the store can't be modified while iterating.
	keys, err := iter.Keys()
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := k.IdentityRateLimits.Remove(ctx, key); err != nil {
			return err
		}
	}
	return nil
}
