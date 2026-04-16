package commons

import (
	"context"

	"sparkdream/x/commons/keeper"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func EndBlocker(ctx context.Context, k keeper.Keeper) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentTime := sdkCtx.BlockTime().Unix()

	// 1. Iterate over Queue: Up to CurrentTime (inclusive).
	// Use EndExclusive with currentTime+1 to ensure entries at exactly currentTime are included.
	// EndInclusive with an empty string as K2 would miss entries at currentTime with non-empty
	// group names, since ("", "") < ("", "anyGroupName") in lexicographic ordering.
	rng := new(collections.Range[collections.Pair[int64, string]]).
		EndExclusive(collections.Join(currentTime+1, ""))

	err := k.MarketTriggerQueue.Walk(ctx, rng, func(key collections.Pair[int64, string]) (bool, error) {
		groupName := key.K2()

		// 2. Fire the Market
		if err := k.TriggerGovernanceMarket(sdkCtx, groupName); err != nil {
			// Log error but don't halt chain.
			sdkCtx.Logger().Error("failed to auto-create market", "group", groupName, "error", err)
		}

		// 3. Remove from Queue (Next one was already scheduled by TriggerGovernanceMarket)
		if err := k.MarketTriggerQueue.Remove(ctx, key); err != nil {
			return true, err
		}

		return false, nil
	})

	return err
}
