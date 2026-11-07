package keeper

import (
	"context"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// reviewDisputeRetryBlocks is how long an overdue review dispute whose
// dismissal failed is parked before it is tried again. Retried every block
// at its place in ReviewDisputes, MaxDisputesResolvedPerBlock such disputes
// at the lowest filed_at filled every block's batch and every overdue
// dispute behind them stayed frozen, the indefinite freeze NAM-201 removed.
const reviewDisputeRetryBlocks int64 = 100

// reviewDisputeScanFactor bounds how many ReviewDisputes entries one block
// reads, parked ones included, as a multiple of MaxDisputesResolvedPerBlock.
const reviewDisputeScanFactor = 10

// reviewDisputeParked reports whether the review dispute at key is parked
// past height.
func (k Keeper) reviewDisputeParked(ctx context.Context, key collections.Pair[int64, string], height int64) bool {
	at, err := k.ParkedReviewDisputeAt.Get(ctx, key)
	return err == nil && at > height
}

// parkReviewDispute parks the review dispute at key until height +
// reviewDisputeRetryBlocks. Called on the block context after the failed
// dismissal's cache context was discarded. Best effort: a failed write
// leaves the dispute retried next block, as before.
func (k Keeper) parkReviewDispute(ctx context.Context, key collections.Pair[int64, string], height int64) {
	k.unparkReviewDispute(ctx, key)
	retryAt := height + reviewDisputeRetryBlocks
	logger := sdk.UnwrapSDKContext(ctx).Logger()
	if err := k.ParkedReviewDisputes.Set(ctx, collections.Join3(retryAt, key.K1(), key.K2())); err != nil {
		logger.Error("parkReviewDispute: ParkedReviewDisputes.Set failed", "name", key.K2(), "err", err)
		return
	}
	if err := k.ParkedReviewDisputeAt.Set(ctx, key, retryAt); err != nil {
		logger.Error("parkReviewDispute: ParkedReviewDisputeAt.Set failed", "name", key.K2(), "err", err)
	}
}

// unparkReviewDispute drops the park of the review dispute at key, if any.
func (k Keeper) unparkReviewDispute(ctx context.Context, key collections.Pair[int64, string]) {
	at, err := k.ParkedReviewDisputeAt.Get(ctx, key)
	if err != nil {
		return
	}
	logger := sdk.UnwrapSDKContext(ctx).Logger()
	if err := k.ParkedReviewDisputes.Remove(ctx, collections.Join3(at, key.K1(), key.K2())); err != nil {
		logger.Error("unparkReviewDispute: ParkedReviewDisputes.Remove failed", "name", key.K2(), "err", err)
	}
	if err := k.ParkedReviewDisputeAt.Remove(ctx, key); err != nil {
		logger.Error("unparkReviewDispute: ParkedReviewDisputeAt.Remove failed", "name", key.K2(), "err", err)
	}
}

// pruneParkedReviewDisputes drops parks whose retry height has come, at most
// MaxDisputesResolvedPerBlock per block. An expired park holds nothing back,
// and one whose dispute the council decided meanwhile would otherwise stay
// in state for good.
func (k Keeper) pruneParkedReviewDisputes(ctx context.Context, height int64) error {
	var expired []collections.Triple[int64, int64, string]
	if err := k.ParkedReviewDisputes.Walk(ctx, collections.NewPrefixUntilTripleRange[int64, int64, string](height),
		func(key collections.Triple[int64, int64, string]) (bool, error) {
			expired = append(expired, key)
			return len(expired) >= MaxDisputesResolvedPerBlock, nil
		}); err != nil {
		return err
	}
	for _, key := range expired {
		if err := k.ParkedReviewDisputes.Remove(ctx, key); err != nil {
			return err
		}
		rev := collections.Join(key.K2(), key.K3())
		if at, err := k.ParkedReviewDisputeAt.Get(ctx, rev); err == nil && at == key.K1() {
			if err := k.ParkedReviewDisputeAt.Remove(ctx, rev); err != nil {
				return err
			}
		}
	}
	return nil
}
