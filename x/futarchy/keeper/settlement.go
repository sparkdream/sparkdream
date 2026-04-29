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
func (k Keeper) computeCreatorResidual(ctx sdk.Context, market types.Market) (math.Int, error) {
	if market.BValue == nil || market.PoolYes == nil || market.PoolNo == nil ||
		market.InitialLiquidity == nil {
		return math.ZeroInt(), fmt.Errorf("market %d missing LMSR fields", market.Index)
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.ZeroInt(), err
	}
	maxExp, err := math.LegacyNewDecFromStr(params.MaxLmsrExponent)
	if err != nil {
		maxExp = types.DefaultMaxExponent
	}

	b := *market.BValue
	qYes := math.LegacyNewDecFromInt(*market.PoolYes)
	qNo := math.LegacyNewDecFromInt(*market.PoolNo)

	switch market.Status {
	case "RESOLVED_YES":
		residual, err := types.CreatorResidualResolved(ctx, b, qYes, qNo, maxExp)
		if err != nil {
			return math.ZeroInt(), err
		}
		return residual.TruncateInt(), nil

	case "RESOLVED_NO":
		residual, err := types.CreatorResidualResolved(ctx, b, qNo, qYes, maxExp)
		if err != nil {
			return math.ZeroInt(), err
		}
		return residual.TruncateInt(), nil

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
		return residual.TruncateInt(), nil

	default:
		return math.ZeroInt(), fmt.Errorf("market %d status %q is not eligible for withdrawal", market.Index, market.Status)
	}
}
