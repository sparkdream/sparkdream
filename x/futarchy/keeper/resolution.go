package keeper

import (
	"fmt"
	stdmath "math"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/futarchy/types"
)

// MaxResolutionsPerBlock caps how many expired markets the EndBlocker
// resolves in one block (FUT-09). Markets past the cap stay in the
// ActiveMarkets index and are picked up by the next block, oldest first.
const MaxResolutionsPerBlock = 100

var halfDec = math.LegacyMustNewDecFromStr("0.5")

// hasTWAP reports whether the market carries the time-weighted price fields
// (every market created by this binary does).
func hasTWAP(market types.Market) bool {
	return market.TwapAccumulator != nil && !market.TwapAccumulator.IsNil() && market.EndBlock > market.StartHeight
}

// accumulateTWAP adds the current LMSR YES price times the blocks elapsed
// since the last accumulation, up to min(now, end_block), and advances the
// accumulation height (FUT-01). It must run before the pools change.
func (k Keeper) accumulateTWAP(ctx sdk.Context, market *types.Market) error {
	if !hasTWAP(*market) {
		return nil
	}
	upTo := ctx.BlockHeight()
	if upTo > market.EndBlock {
		upTo = market.EndBlock
	}
	elapsed := upTo - market.TwapLastHeight
	if elapsed <= 0 {
		return nil
	}
	price, err := types.SettlementPriceYes(ctx, *market.BValue,
		math.LegacyNewDecFromInt(*market.PoolYes), math.LegacyNewDecFromInt(*market.PoolNo))
	if err != nil {
		return err
	}
	acc := market.TwapAccumulator.Add(price.MulInt64(elapsed))
	market.TwapAccumulator = &acc
	market.TwapLastHeight = upTo
	return nil
}

// resolutionOutcome decides a market's outcome.
//
// FUT-01: the outcome is the time-weighted average of the LMSR YES price over
// [start_height, end_block): YES if above 1/2, NO if below, INVALID at
// exactly 1/2 (which includes a market nobody traded). Resolving on the
// final pool state let whoever traded last flip the outcome with a
// guaranteed profit; moving the average needs the price held away from fair
// for a large share of the window, against every other trader.
//
// Markets without TWAP fields (none are created by this binary) fall back
// to the final pool comparison.
func (k Keeper) resolutionOutcome(ctx sdk.Context, market *types.Market) (string, error) {
	if market.PoolYes.IsZero() && market.PoolNo.IsZero() {
		return "RESOLVED_INVALID", nil
	}
	if !hasTWAP(*market) {
		switch {
		case market.PoolYes.GT(*market.PoolNo):
			return "RESOLVED_YES", nil
		case market.PoolNo.GT(*market.PoolYes):
			return "RESOLVED_NO", nil
		default:
			return "RESOLVED_INVALID", nil
		}
	}
	if err := k.accumulateTWAP(ctx.WithBlockHeight(market.EndBlock), market); err != nil {
		return "", err
	}
	twap := market.TwapAccumulator.QuoInt64(market.EndBlock - market.StartHeight)
	switch {
	case twap.GT(halfDec):
		return "RESOLVED_YES", nil
	case twap.LT(halfDec):
		return "RESOLVED_NO", nil
	default:
		return "RESOLVED_INVALID", nil
	}
}

// ResolveExpiredMarkets resolves up to MaxResolutionsPerBlock markets whose
// end_block has passed. It never returns an error (FUT-04 addendum): each
// market resolves in its own CacheContext and a failure is logged and
// retried next block, so a bad market can never halt the chain.
func (k Keeper) ResolveExpiredMarkets(ctx sdk.Context) {
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndInclusive(collections.Join(ctx.BlockHeight(), uint64(stdmath.MaxUint64)))

	var keys []collections.Pair[int64, uint64]
	if err := k.ActiveMarkets.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		keys = append(keys, key)
		return len(keys) >= MaxResolutionsPerBlock, nil
	}); err != nil {
		ctx.Logger().Error("futarchy: walking expired markets failed", "error", err)
	}

	for _, key := range keys {
		cacheCtx, write := ctx.CacheContext()
		if err := k.resolveMarket(cacheCtx, key); err != nil {
			ctx.Logger().Error("futarchy: market resolution failed; retrying next block",
				"market_id", key.K2(), "error", err)
			continue
		}
		write()
	}
}

// resolveMarket resolves one expired market, returns the creator's residual
// and fires the resolution hook.
func (k Keeper) resolveMarket(ctx sdk.Context, key collections.Pair[int64, uint64]) error {
	marketID := key.K2()
	market, err := k.Market.Get(ctx, marketID)
	if err != nil || market.Status != "ACTIVE" {
		// Orphan or stale index entry.
		return k.ActiveMarkets.Remove(ctx, key)
	}
	if market.BValue == nil || market.PoolYes == nil || market.PoolNo == nil {
		return fmt.Errorf("market %d missing LMSR fields", marketID)
	}

	resolution, err := k.resolutionOutcome(ctx, &market)
	if err != nil {
		return err
	}
	market.Status = resolution
	market.ResolutionHeight = ctx.BlockHeight()

	// FUTARCHY-S2-1: INVALID resolutions with outstanding shares need a
	// settlement price snapshot so holders can redeem at LMSR-implied
	// price. RESOLVED_YES/NO use winner-pays-1:1 and don't need this.
	if resolution == "RESOLVED_INVALID" && (market.PoolYes.IsPositive() || market.PoolNo.IsPositive()) {
		pYes, err := types.SettlementPriceYes(ctx, *market.BValue,
			math.LegacyNewDecFromInt(*market.PoolYes), math.LegacyNewDecFromInt(*market.PoolNo))
		if err != nil {
			return err
		}
		market.SettlementPriceYes = &pYes
	}

	if err := k.Market.Set(ctx, marketID, market); err != nil {
		return err
	}
	if err := k.ActiveMarkets.Remove(ctx, key); err != nil {
		return err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"market_resolved",
			sdk.NewAttribute("market_id", fmt.Sprintf("%d", marketID)),
			sdk.NewAttribute("end_block", fmt.Sprintf("%d", key.K1())),
			sdk.NewAttribute("outcome", resolution),
			sdk.NewAttribute("twap_accumulator", decString(market.TwapAccumulator)),
		),
	)

	// FUT-02: return the creator's residual now. Programmatic creators
	// (x/commons confidence markets) never sign WithdrawLiquidity, so the
	// residual used to stay in the module account forever. A failure leaves
	// the residual for a manual WithdrawLiquidity and does not undo the
	// resolution.
	refundCtx, writeRefund := ctx.CacheContext()
	if err := k.refundCreatorResidual(refundCtx, marketID); err != nil {
		ctx.Logger().Error("futarchy: creator residual refund failed; left for WithdrawLiquidity",
			"market_id", marketID, "error", err)
	} else {
		writeRefund()
	}

	// FUT-07: the hook fires for every resolution, including INVALID
	// (winner "invalid"), so listeners can clean up their link to the market.
	if k.late.hooks != nil {
		winner := "invalid"
		switch resolution {
		case "RESOLVED_YES":
			winner = "yes"
		case "RESOLVED_NO":
			winner = "no"
		}
		hookCtx, writeHook := ctx.CacheContext()
		if err := k.late.hooks.AfterMarketResolved(hookCtx, marketID, winner); err != nil {
			ctx.Logger().Error("futarchy hook failed", "market_id", marketID, "error", err)
		} else {
			writeHook()
		}
	}
	return nil
}

// refundCreatorResidual pays the market creator whatever part of the LMSR
// residual has not been withdrawn yet.
func (k Keeper) refundCreatorResidual(ctx sdk.Context, marketID uint64) error {
	market, err := k.Market.Get(ctx, marketID)
	if err != nil {
		return err
	}
	if market.LiquidityWithdrawn == nil {
		zero := math.ZeroInt()
		market.LiquidityWithdrawn = &zero
	}
	residual, err := k.computeCreatorResidual(ctx, market)
	if err != nil {
		return err
	}
	owed := residual.Sub(*market.LiquidityWithdrawn)
	if !owed.IsPositive() {
		return nil
	}
	creator, err := sdk.AccAddressFromBech32(market.Creator)
	if err != nil {
		return err
	}
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, creator, sdk.NewCoins(sdk.NewCoin(market.Denom, owed))); err != nil {
		return err
	}
	withdrawn := market.LiquidityWithdrawn.Add(owed)
	market.LiquidityWithdrawn = &withdrawn
	if err := k.Market.Set(ctx, marketID, market); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"liquidity_withdrawn",
			sdk.NewAttribute("market_id", fmt.Sprintf("%d", marketID)),
			sdk.NewAttribute("creator", market.Creator),
			sdk.NewAttribute("amount", owed.String()),
			sdk.NewAttribute("automatic", "true"),
		),
	)
	return nil
}

func decString(d *math.LegacyDec) string {
	if d == nil || d.IsNil() {
		return ""
	}
	return d.String()
}
