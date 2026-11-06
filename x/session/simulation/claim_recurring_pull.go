package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// SimulateMsgClaimRecurringPull has a live sim grantee claim a recurring
// pull whose claim the handler accepts now (a due period, under the
// per-UTC-day max_per_epoch ceiling; an underfunded granter pauses the grant
// and a closed window completes it, both successful claims). When none is
// claimable the op first creates a pull whose start is backdated within the
// handler's one-hour allowance, so its first period is already due.
func SimulateMsgClaimRecurringPull(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	label := &types.MsgClaimRecurringPull{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		now := e.ctx.BlockTime().Unix()
		pulls, err := e.grants(func(g types.Grant) bool {
			rp := g.GetRecurringPull()
			return g.Type == types.GrantType_GRANT_TYPE_RECURRING_PULL && actionable(g) && rp != nil &&
				rp.LastClaimAdvance+rp.PeriodSeconds <= now
		})
		if err != nil {
			return noOpErr(label, err)
		}
		for _, i := range e.r.Perm(len(pulls)) {
			g := pulls[i]
			grantee, ok := e.account(g.Grantee)
			if !ok {
				continue
			}
			msg := &types.MsgClaimRecurringPull{Grantee: g.Grantee, GrantId: g.Id}
			if e.dryRun(msg) != nil {
				continue // not due under max_per_epoch, or a hook vetoes it
			}
			return e.deliver(grantee, msg, math.ZeroInt())
		}

		// Prerequisite: a recurring pull (half are created due at once).
		created, g, ok, err := e.createGrant(kindRecurringPull)
		if err != nil {
			return created, nil, err
		}
		if !ok {
			return noOp(label, "no claimable recurring pull and none could be created")
		}
		if grantee, live := e.account(g.Grantee); live {
			msg := &types.MsgClaimRecurringPull{Grantee: g.Grantee, GrantId: g.Id}
			if e.dryRun(msg) == nil {
				return e.deliver(grantee, msg, math.ZeroInt())
			}
		}
		created.Comment = "created a recurring pull; its first period is not due yet"
		return created, nil, nil
	})
}
