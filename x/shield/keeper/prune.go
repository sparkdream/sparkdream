package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// pruneStaleState removes stale entries from various collections.
// Called once per epoch boundary from EndBlocker.
func (k Keeper) pruneStaleState(ctx context.Context, currentEpoch uint64) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return
	}

	// Guard against underflow on early chain startup.
	var cutoffEpoch uint64
	if currentEpoch > uint64(params.MaxPendingEpochs) {
		cutoffEpoch = currentEpoch - uint64(params.MaxPendingEpochs)
	}

	// 1. Prune identity rate limits from old epochs.
	if currentEpoch > 1 {
		if err := k.PruneIdentityRateLimits(ctx, currentEpoch-1); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("pruneStaleState: k.PruneIdentityRateLimits failed", "err", err)
		}
		if err := k.PruneIdentityEpochFees(ctx, currentEpoch-1); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("pruneStaleState: k.PruneIdentityEpochFees failed", "err", err)
		}
	}

	// 2. Prune epoch-scoped nullifiers.
	if cutoffEpoch > 0 {
		if err := k.PruneEpochScopedNullifiers(ctx, cutoffEpoch); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("pruneStaleState: k.PruneEpochScopedNullifiers failed", "err", err)
		}
	}

	// 3. Prune old DayFunding entries (keep current and previous day only).
	//    Days are bucketed by block time, like the funding ledger itself.
	if currentDay := fundingDay(sdkCtx); currentDay > 1 {
		if err := k.PruneDayFundings(ctx, currentDay-1); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("pruneStaleState: k.PruneDayFundings failed", "err", err)
		}
	}

	// 4. Prune old decryption keys and shares.
	if cutoffEpoch > 0 {
		if err := k.PruneDecryptionState(ctx, cutoffEpoch); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("pruneStaleState: k.PruneDecryptionState failed", "err", err)
		}
	}
}
