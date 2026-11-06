package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// SimulateMsgAcceptGrant has the live sim grantee of a PENDING access-class
// grant accept it (the granter's consent deposit is refunded). When no
// pending grant has a live sim grantee, the op first creates a session key
// or sign-in with a real MsgCreateGrant; that grant is PENDING unless the
// grantee already allows the granter, in which case the op is a no-op.
func SimulateMsgAcceptGrant(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	label := &types.MsgAcceptGrant{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		all, err := e.grants(func(g types.Grant) bool { return g.Status == types.GrantStatus_GRANT_STATUS_PENDING })
		if err != nil {
			return noOpErr(label, err)
		}
		for _, i := range e.r.Perm(len(all)) {
			g := all[i]
			grantee, ok := e.account(g.Grantee)
			if !ok {
				continue
			}
			msg := &types.MsgAcceptGrant{Grantee: g.Grantee, GrantId: g.Id}
			if e.dryRun(msg) != nil {
				continue
			}
			return e.deliver(grantee, msg, math.ZeroInt())
		}

		// Prerequisite: a pending grant to accept.
		kind := kindSessionKey
		if e.r.Intn(2) == 0 {
			kind = kindSignIn
		}
		created, g, ok, err := e.createGrant(kind)
		if err != nil {
			return created, nil, err
		}
		if !ok {
			return noOp(label, "no pending grant with a live sim grantee and none could be created")
		}
		grantee, live := e.account(g.Grantee)
		if !live || g.Status != types.GrantStatus_GRANT_STATUS_PENDING {
			return created, nil, nil
		}
		return e.submit(grantee, &types.MsgAcceptGrant{Grantee: g.Grantee, GrantId: g.Id}, math.ZeroInt())
	})
}
