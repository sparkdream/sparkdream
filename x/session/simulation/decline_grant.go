package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// SimulateMsgDeclineGrant has the live sim grantee of a pending grant of
// any type decline it (a declined oneshot refunds the granter's deposit).
// When no grant has a live sim grantee, the op first creates one with a
// real MsgCreateGrant and declines it.
func SimulateMsgDeclineGrant(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	label := &types.MsgDeclineGrant{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		all, err := e.grants(pending)
		if err != nil {
			return noOpErr(label, err)
		}
		for _, i := range e.r.Perm(len(all)) {
			g := all[i]
			grantee, ok := e.account(g.Grantee)
			if !ok {
				continue
			}
			msg := &types.MsgDeclineGrant{Grantee: g.Grantee, GrantId: g.Id}
			if e.dryRun(msg) != nil {
				continue
			}
			return e.deliver(grantee, msg, math.ZeroInt())
		}

		// Prerequisite: a grant to decline.
		created, g, ok, err := e.createGrant(grantKind(e.r.Intn(int(numGrantKinds))))
		if err != nil {
			return created, nil, err
		}
		if !ok {
			return noOp(label, "no grant with a live sim grantee and none could be created")
		}
		grantee, live := e.account(g.Grantee)
		if !live {
			return created, nil, nil
		}
		return e.submit(grantee, &types.MsgDeclineGrant{Grantee: g.Grantee, GrantId: g.Id}, math.ZeroInt())
	})
}
