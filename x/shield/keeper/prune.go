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
		_ = k.PruneIdentityRateLimits(ctx, currentEpoch-1)
	}

	// 2. Prune epoch-scoped nullifiers.
	if cutoffEpoch > 0 {
		_ = k.PruneEpochScopedNullifiers(ctx, cutoffEpoch)
	}

	// 3. Prune old DayFunding entries (keep current and previous day only).
	currentDay := uint64(sdkCtx.BlockHeight()) / 14400
	if currentDay > 1 {
		_ = k.PruneDayFundings(ctx, currentDay-1)
	}

	// 4. Prune old decryption keys and shares.
	if cutoffEpoch > 0 {
		_ = k.PruneDecryptionState(ctx, cutoffEpoch)
	}
}
