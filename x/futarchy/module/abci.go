package futarchy

import (
	"context"
	"fmt"
	stdmath "math"

	"sparkdream/x/futarchy/keeper"
	"sparkdream/x/futarchy/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func EndBlocker(ctx context.Context, k keeper.Keeper) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	// 1. Define Range
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndInclusive(collections.Join(currentHeight, uint64(stdmath.MaxUint64)))

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
		} else if poolNo.GT(poolYes) {
			resolution = "RESOLVED_NO"
		} else {
			resolution = "RESOLVED_INVALID"
		}

		// 4. Update State
		market.Status = resolution
		market.ResolutionHeight = currentHeight

		// FUTARCHY-S2-1: INVALID resolutions with outstanding shares need a
		// settlement price snapshot so holders can redeem at LMSR-implied
		// price. RESOLVED_YES/NO use winner-pays-1:1 and don't need this.
		if resolution == "RESOLVED_INVALID" && (market.PoolYes.IsPositive() || market.PoolNo.IsPositive()) {
			params, perr := k.Params.Get(ctx)
			if perr != nil {
				return true, perr
			}
			maxExp, mErr := math.LegacyNewDecFromStr(params.MaxLmsrExponent)
			if mErr != nil {
				maxExp = types.DefaultMaxExponent
			}
			qYes := math.LegacyNewDecFromInt(*market.PoolYes)
			qNo := math.LegacyNewDecFromInt(*market.PoolNo)
			pYes, pErr := types.SettlementPriceYes(sdkCtx, *market.BValue, qYes, qNo, maxExp)
			if pErr != nil {
				return true, pErr
			}
			market.SettlementPriceYes = &pYes
		}

		if err := k.Market.Set(ctx, marketId, market); err != nil {
			return true, err
		}

		// 5. Remove from Active Queue
		if err := k.ActiveMarkets.Remove(ctx, key); err != nil {
			return true, err
		}

		// 6. Trigger Hooks (skip for INVALID resolutions — no winner to announce)
		if k.Hooks != nil && resolution != "RESOLVED_INVALID" {
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
