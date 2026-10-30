package keeper

import (
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/futarchy/types"
)

// computeCreatorResidual returns the SPARK amount the market creator is owed
// from the futarchy module account given the market's current state. Three
// regimes (FUTARCHY-S2-1):
//
//  1. RESOLVED_YES / RESOLVED_NO — winning shareholders redeem 1:1, so the
//     creator's residual is C(qY, qN) - q_winner = b * ln(1 + e^((q_l-q_w)/b)).
//
//  2. CANCELLED / RESOLVED_INVALID with non-zero pools — every share is
//     redeemed at the LMSR-implied price stored on the market. The residual
//     equals b * H(p_yes), the entropy of the implied distribution. The
//     SettlementPriceYes field MUST be populated by the caller (CancelMarket
//     or ABCI invalid-resolution) before this path runs.
//
//  3. RESOLVED_INVALID with zero pools — no trades, full subsidy returns.
//
// CANCELLED here is callable from CancelMarket *before* it persists status,
// so the function takes a marketStatus override-equivalent: the market struct
// passed in must already have its eventual SettlementPriceYes (if applicable).
// The result is already capped where the math bounds it, so callers must not
// cap it at InitialLiquidity again.
func (k Keeper) computeCreatorResidual(ctx sdk.Context, market types.Market) (math.Int, error) {
	if market.BValue == nil || market.PoolYes == nil || market.PoolNo == nil ||
		market.InitialLiquidity == nil {
		return math.ZeroInt(), fmt.Errorf("market %d missing LMSR fields", market.Index)
	}

	b := *market.BValue
	qYes := math.LegacyNewDecFromInt(*market.PoolYes)
	qNo := math.LegacyNewDecFromInt(*market.PoolNo)

	switch market.Status {
	case "RESOLVED_YES", "RESOLVED_NO":
		qWin, qLose := qYes, qNo
		if market.Status == "RESOLVED_NO" {
			qWin, qLose = qNo, qYes
		}
		residual, err := types.CreatorResidualResolved(ctx, b, qWin, qLose)
		if err != nil {
			return math.ZeroInt(), err
		}
		out := residual.TruncateInt()
		// With the winner holding at least as many shares as the loser the
		// residual is mathematically <= InitialLiquidity; cap defensively
		// against rounding. Under time-weighted resolution (FUT-01) the
		// winner may hold fewer shares, and then the residual C(q) - q_win
		// legitimately exceeds the subsidy; it is still backed by C(q).
		if qWin.GTE(qLose) && out.GT(*market.InitialLiquidity) {
			out = *market.InitialLiquidity
		}
		return out, nil

	case "CANCELLED", "RESOLVED_INVALID":
		// Zero pools: trivial — full subsidy refund. Settlement price is
		// undefined / not needed because there are no shares to redeem.
		if qYes.IsZero() && qNo.IsZero() {
			return *market.InitialLiquidity, nil
		}
		// Non-zero pools require a settlement price snapshot.
		if market.SettlementPriceYes == nil {
			return math.ZeroInt(), fmt.Errorf("market %d in %s with non-zero pools but no settlement price", market.Index, market.Status)
		}
		residual, err := types.CreatorResidualSettled(ctx, b, *market.SettlementPriceYes)
		if err != nil {
			return math.ZeroInt(), err
		}
		// b*H(p) <= b*ln(2) = InitialLiquidity; cap against rounding.
		out := residual.TruncateInt()
		if out.GT(*market.InitialLiquidity) {
			out = *market.InitialLiquidity
		}
		return out, nil

	default:
		return math.ZeroInt(), fmt.Errorf("market %d status %q is not eligible for withdrawal", market.Index, market.Status)
	}
}
