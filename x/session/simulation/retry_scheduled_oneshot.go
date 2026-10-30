package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// SimulateMsgRetryScheduledOneshot re-arms a PAUSED_INSUFFICIENT_FUNDS
// scheduled oneshot, signed by its live sim granter or grantee. A oneshot
// pauses only at fire time (EndBlocker), so when none is paused the op
// creates an oneshot transfer larger than the granter holds: it pauses when
// it fires in a later block, and a later call retries it.
func SimulateMsgRetryScheduledOneshot(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	label := &types.MsgRetryScheduledOneshot{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		paused, err := e.grants(func(g types.Grant) bool {
			return g.Type == types.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT &&
				g.Status == types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS
		})
		if err != nil {
			return noOpErr(label, err)
		}
		for _, i := range e.r.Perm(len(paused)) {
			g := paused[i]
			callers := []string{g.Granter, g.Grantee}
			if e.r.Intn(2) == 0 {
				callers[0], callers[1] = callers[1], callers[0]
			}
			for _, c := range callers {
				acc, ok := e.account(c)
				if !ok {
					continue
				}
				msg := &types.MsgRetryScheduledOneshot{Caller: c, GrantId: g.Id}
				if e.dryRun(msg) != nil {
					continue
				}
				return e.deliver(acc, msg, math.ZeroInt())
			}
		}

		// Prerequisite: an underfunded oneshot transfer that pauses when it
		// fires.
		granter, grantee, ok := e.pair(math.NewInt(100_000))
		if !ok {
			return noOp(label, "no paused oneshot and no pair to schedule one")
		}
		msg, spent, why, err := e.buildGrant(kindOneshotTransfer, granter, grantee)
		if err != nil {
			return noOpErr(label, err)
		}
		if why != "" {
			return noOp(label, "no paused oneshot: "+why)
		}
		if t := msg.GetScheduledOneshot().GetTransfer(); t != nil {
			t.Amount = sdk.NewCoin(e.denom(), e.spendable(granter).MulRaw(2).AddRaw(1))
		}
		created, ok, err := e.ensure(granter, msg, spent)
		if err != nil || !ok {
			if err == nil {
				return noOp(label, "no paused oneshot and an underfunded one cannot be scheduled")
			}
			return created, nil, err
		}
		created.Comment = "scheduled an underfunded oneshot transfer to pause at fire time"
		return created, nil, nil
	})
}
