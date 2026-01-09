package futarchy

import (
	"context"
	"fmt"
	"math"

	"sparkdream/x/futarchy/keeper"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func EndBlocker(ctx context.Context, k keeper.Keeper) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	// 1. Define Range
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndInclusive(collections.Join(currentHeight, uint64(math.MaxUint64)))

	// 2. Walk only the expired markets
	err := k.ActiveMarkets.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		endBlock := key.K1()
		marketId := key.K2()

		// Fetch market
		market, err := k.Market.Get(ctx, marketId)
		if err != nil {
			// Clean up orphan index
			k.ActiveMarkets.Remove(ctx, key)
			return false, nil
		}

		// Double check status
		if market.Status != "ACTIVE" {
			k.ActiveMarkets.Remove(ctx, key)
			return false, nil
		}

		// 3. Resolve Market
		poolYes := *market.PoolYes
		poolNo := *market.PoolNo
		resolution := "RESOLVED_INVALID"

		if poolYes.IsZero() && poolNo.IsZero() {
			resolution = "RESOLVED_INVALID"
		} else if poolYes.GT(poolNo) {
			resolution = "RESOLVED_YES"
		} else {
			resolution = "RESOLVED_NO"
		}

		// 4. Update State
		market.Status = resolution
		market.ResolutionHeight = currentHeight
		if err := k.Market.Set(ctx, marketId, market); err != nil {
			return true, err
		}

		// 5. Remove from Active Queue
		if err := k.ActiveMarkets.Remove(ctx, key); err != nil {
			return true, err
		}

		// 6. Trigger Hooks
		if k.Hooks != nil {
			// winner is "yes" or "no" based on resolution string
			winnerShort := "no"
			if resolution == "RESOLVED_YES" {
				winnerShort = "yes"
			}
			if err := k.Hooks.AfterMarketResolved(ctx, marketId, winnerShort); err != nil {
				// Log error but don't halt chain? Or return true to retry?
				// Usually, we log and continue for hooks to isolate failures.
				sdkCtx.Logger().Error("futarchy hook failed", "market_id", marketId, "error", err)
			}
		}

		// Emit Event
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"market_resolved",
				sdk.NewAttribute("market_id", fmt.Sprintf("%d", marketId)),
				sdk.NewAttribute("end_block", fmt.Sprintf("%d", endBlock)),
				sdk.NewAttribute("outcome", resolution),
			),
		)

		return false, nil
	})

	return err
}
