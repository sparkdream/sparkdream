package simulation

import (
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// SimulateMsgPullAllowance picks a random SPENDING_ALLOWANCE grant
// with budget headroom in the current rolling window and submits a
// pull. The amount is bounded by `params.MinPullAmount` (floor) and
// `max_per_period - spent_in_current_period` (ceiling). The recipient
// is some sim account other than the granter (the handler rejects
// recipient == granter).
//
// NoOps when:
//   - No SPENDING_ALLOWANCE grants exist.
//   - The selected grant's grantee isn't in the sim account set.
//   - No headroom remains in the budget (full period spent).
func SimulateMsgPullAllowance(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgPullAllowance{})

		now := ctx.BlockTime().Unix()
		grant, found := findRandomGrantOfType(
			r, ctx, k, types.GrantType_GRANT_TYPE_SPENDING_ALLOWANCE,
			func(g types.Grant) bool {
				if g.Status != types.GrantStatus_GRANT_STATUS_ACTIVE &&
					g.Status != types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS {
					return false
				}
				if !g.ExpiresAt.After(ctx.BlockTime()) {
					return false
				}
				sa := g.GetSpendingAllowance()
				return sa != nil
			},
		)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no pullable allowance grant"), nil, nil
		}

		granteeAcc, ok := findSimAccount(accs, grant.Grantee)
		if !ok {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "grantee not a sim account"), nil, nil
		}

		sa := grant.GetSpendingAllowance()

		// Compute remaining headroom: if period has rolled, full budget;
		// else max_per_period - spent_in_current_period.
		remaining := sa.MaxPerPeriod.Amount
		if now < sa.CurrentPeriodStart+sa.PeriodSeconds {
			remaining = sa.MaxPerPeriod.Amount.Sub(sa.SpentInCurrentPeriod.Amount)
		}

		// Clamp headroom to the granter's spendable balance. The handler debits
		// the granter's bank account, so a pull above their balance would fail
		// delivery (and fail the whole simulation). Budget headroom alone is not
		// enough — the granter may have spent SPARK on fees since the grant was
		// created.
		granterAddr, err := sdk.AccAddressFromBech32(grant.Granter)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "invalid granter address"), nil, nil
		}
		granterBal := bk.SpendableCoins(ctx, granterAddr).AmountOf(sa.Denom)
		if granterBal.LT(remaining) {
			remaining = granterBal
		}

		// Min pull amount from params.
		params, err := k.Params.Get(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get params"), nil, nil
		}
		minPull, ok := math.NewIntFromString(params.MinPullAmount)
		if !ok {
			minPull = math.NewInt(1000)
		}
		if remaining.LT(minPull) {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no headroom in allowance budget"), nil, nil
		}

		// Pick a pull amount in [minPull, remaining]. Random pick of
		// the size of the headroom — keeps the sim from always
		// draining in one shot.
		span := remaining.Sub(minPull)
		jitter := math.ZeroInt()
		if span.IsPositive() {
			jitter = math.NewInt(r.Int63n(span.Int64() + 1))
		}
		pullAmt := minPull.Add(jitter)

		// Pick a recipient different from the granter.
		var recipient string
		for _, idx := range r.Perm(len(accs)) {
			cand := accs[idx].Address.String()
			if cand != grant.Granter {
				recipient = cand
				break
			}
		}
		if recipient == "" {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no eligible recipient"), nil, nil
		}

		msg := &types.MsgPullAllowance{
			Grantee:   grant.Grantee,
			GrantId:   grant.Id,
			Recipient: recipient,
			Amount:    sdk.NewCoin(sa.Denom, pullAmt),
		}

		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      granteeAcc,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		return simulation.GenAndDeliverTxWithRandFees(opMsg)
	}
}
