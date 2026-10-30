package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/futarchy/types"
)

// solvencyTolerancePerMarket absorbs the last-digit rounding of the LMSR
// math per market (base units). Truncations in Trade, Redeem and the
// residual all round in the module's favour, so a real shortfall shows up
// far above this.
var solvencyTolerancePerMarket = math.NewInt(10)

// supplyReader is the optional bank capability the solvency invariant needs
// to count outstanding (unredeemed) shares. The real bank keeper has it; the
// unit-test bank mocks need not.
type supplyReader interface {
	GetSupply(ctx context.Context, denom string) sdk.Coin
}

// RegisterInvariants registers the futarchy invariants. x/crisis is not wired
// on chain; app's TestInvariants runs every module's RegisterInvariants.
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
	ir.RegisterRoute(types.ModuleName, "solvency", SolvencyInvariant(k))
}

// SolvencyInvariant checks spec §12.4 (FUT-05): for every collateral denom,
// the futarchy module account holds at least the sum of what it still owes
// across all markets:
//
//   - ACTIVE: C(q_yes, q_no), the LMSR cost, which covers both the worst-case
//     1:1 winner payout and the creator residual.
//   - RESOLVED_YES / RESOLVED_NO: the outstanding winning share supply (1:1)
//     plus the creator residual not yet withdrawn.
//   - CANCELLED / RESOLVED_INVALID: outstanding YES supply * p_yes plus NO
//     supply * (1 - p_yes), plus the unwithdrawn residual.
//
// The module account is shared by every market, so a shortfall in one market
// would be paid out of another's collateral.
func SolvencyInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		sr, ok := k.bankKeeper.(supplyReader)
		if !ok {
			return sdk.FormatInvariant(types.ModuleName, "solvency", "skipped: bank keeper exposes no GetSupply"), false
		}
		owed, msg, broken := k.outstandingObligations(ctx, sr)
		if broken {
			return sdk.FormatInvariant(types.ModuleName, "solvency", msg), true
		}
		moduleAddr := k.authKeeper.GetModuleAddress(types.ModuleName)
		var problems string
		for denom, need := range owed {
			have := k.bankKeeper.GetBalance(ctx, moduleAddr, denom).Amount
			if have.LT(need) {
				problems += fmt.Sprintf("denom %s: module balance %s < obligations %s\n", denom, have, need)
			}
		}
		return sdk.FormatInvariant(types.ModuleName, "solvency", problems), problems != ""
	}
}

// outstandingObligations sums, per collateral denom, what the module still
// owes (minus the per-market rounding tolerance).
func (k Keeper) outstandingObligations(ctx sdk.Context, sr supplyReader) (map[string]math.Int, string, bool) {
	owed := map[string]math.Int{}
	var problem string
	err := k.Market.Walk(ctx, nil, func(id uint64, m types.Market) (bool, error) {
		if m.BValue == nil || m.PoolYes == nil || m.PoolNo == nil || m.InitialLiquidity == nil {
			problem = fmt.Sprintf("market %d missing LMSR fields", id)
			return true, nil
		}
		withdrawn := math.ZeroInt()
		if m.LiquidityWithdrawn != nil {
			withdrawn = *m.LiquidityWithdrawn
		}
		qYes := math.LegacyNewDecFromInt(*m.PoolYes)
		qNo := math.LegacyNewDecFromInt(*m.PoolNo)
		yesSupply := math.LegacyNewDecFromInt(sr.GetSupply(ctx, fmt.Sprintf("f/%d/yes", id)).Amount)
		noSupply := math.LegacyNewDecFromInt(sr.GetSupply(ctx, fmt.Sprintf("f/%d/no", id)).Amount)

		var need math.LegacyDec
		switch m.Status {
		case "ACTIVE":
			cost, err := types.CalculateLMSRCost(ctx, *m.BValue, qYes, qNo)
			if err != nil {
				return true, err
			}
			need = cost.Sub(math.LegacyNewDecFromInt(withdrawn))
		case "RESOLVED_YES", "RESOLVED_NO", "CANCELLED", "RESOLVED_INVALID":
			residual, err := k.computeCreatorResidual(ctx, m)
			if err != nil {
				return true, err
			}
			unpaid := residual.Sub(withdrawn)
			if unpaid.IsNegative() {
				unpaid = math.ZeroInt()
			}
			need = math.LegacyNewDecFromInt(unpaid)
			switch m.Status {
			case "RESOLVED_YES":
				need = need.Add(yesSupply)
			case "RESOLVED_NO":
				need = need.Add(noSupply)
			default:
				if m.SettlementPriceYes != nil {
					p := *m.SettlementPriceYes
					need = need.Add(yesSupply.Mul(p)).Add(noSupply.Mul(math.LegacyOneDec().Sub(p)))
				}
			}
		default:
			problem = fmt.Sprintf("market %d has unknown status %q", id, m.Status)
			return true, nil
		}

		needInt := need.TruncateInt().Sub(solvencyTolerancePerMarket)
		if needInt.IsNegative() {
			needInt = math.ZeroInt()
		}
		cur, ok := owed[m.Denom]
		if !ok {
			cur = math.ZeroInt()
		}
		owed[m.Denom] = cur.Add(needInt)
		return false, nil
	})
	if err != nil {
		return nil, err.Error(), true
	}
	if problem != "" {
		return nil, problem, true
	}
	return owed, "", false
}
