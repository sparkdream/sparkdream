package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// buildPull builds a MsgPullAllowance on allowance g: an amount between
// min_pull_amount and the period's headroom (capped by what the granter
// holds, so most pulls transfer; an underfunded granter pauses the grant,
// which the handler also reports as a success), to a recipient the grant
// allows that is not the granter.
func (e *simEnv) buildPull(g types.Grant) (*types.MsgPullAllowance, string) {
	sa := g.GetSpendingAllowance()
	if sa == nil || !actionable(g) || !g.ExpiresAt.After(e.ctx.BlockTime()) {
		return nil, "allowance is not pullable"
	}
	minPull, ok := math.NewIntFromString(e.params.MinPullAmount)
	if !ok || minPull.IsNegative() {
		return nil, "min_pull_amount does not parse"
	}
	minPull = math.MaxInt(minPull, math.OneInt())
	headroom := sa.MaxPerPeriod.Amount
	if e.ctx.BlockTime().Unix() < sa.CurrentPeriodStart+sa.PeriodSeconds {
		headroom = headroom.Sub(sa.SpentInCurrentPeriod.Amount)
	}
	if bal := e.spendableOf(g.Granter); bal.GTE(minPull) && e.r.Intn(8) != 0 {
		headroom = math.MinInt(headroom, bal)
	}
	if headroom.LT(minPull) {
		return nil, "no headroom left in the allowance period"
	}
	amount := minPull
	if span := headroom.Sub(minPull); span.IsPositive() {
		if extra, err := simtypes.RandPositiveInt(e.r, span); err == nil {
			amount = amount.Add(extra)
		}
	}
	var recipients []string
	if len(sa.AllowedRecipients) > 0 {
		recipients = sa.AllowedRecipients
	} else {
		for _, acc := range e.live(nil) {
			recipients = append(recipients, acc.Address.String())
		}
	}
	var eligible []string
	for _, r := range recipients {
		if r != g.Granter {
			eligible = append(eligible, r)
		}
	}
	recipient, found := pick(e.r, eligible)
	if !found {
		return nil, "no recipient other than the granter"
	}
	return &types.MsgPullAllowance{
		Grantee:   g.Grantee,
		GrantId:   g.Id,
		Recipient: recipient,
		Amount:    sdk.NewCoin(sa.Denom, amount),
	}, ""
}

// SimulateMsgPullAllowance has a live sim grantee pull from a spending
// allowance. When none is pullable the op first creates one with a real
// MsgCreateGrant and pulls from it.
func SimulateMsgPullAllowance(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	label := &types.MsgPullAllowance{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		allowances, err := e.grants(func(g types.Grant) bool {
			return g.Type == types.GrantType_GRANT_TYPE_SPENDING_ALLOWANCE && actionable(g)
		})
		if err != nil {
			return noOpErr(label, err)
		}
		try := func(g types.Grant) (bool, simtypes.OperationMsg, error) {
			grantee, ok := e.account(g.Grantee)
			if !ok {
				return false, simtypes.OperationMsg{}, nil
			}
			msg, why := e.buildPull(g)
			if why != "" || e.dryRun(msg) != nil {
				return false, simtypes.OperationMsg{}, nil
			}
			opMsg, _, err := e.deliver(grantee, msg, math.ZeroInt())
			return true, opMsg, err
		}
		for _, i := range e.r.Perm(len(allowances)) {
			if done, opMsg, err := try(allowances[i]); done || err != nil {
				return opMsg, nil, err
			}
		}

		// Prerequisite: a fresh allowance.
		created, g, ok, err := e.createGrant(kindAllowance)
		if err != nil {
			return created, nil, err
		}
		if !ok {
			return noOp(label, "no pullable allowance and none could be created")
		}
		if done, opMsg, err := try(g); done || err != nil {
			return opMsg, nil, err
		}
		created.Comment = "created an allowance that cannot be pulled yet"
		return created, nil, nil
	})
}
