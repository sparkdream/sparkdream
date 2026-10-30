package simulation

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// SimulateMsgRevokeSession revokes a session key through the legacy facade:
// a live sim granter's active session key, or, when there is none, one the
// op first creates with a real MsgCreateSession.
func SimulateMsgRevokeSession(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	label := &types.MsgRevokeSession{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		sessions, err := e.grants(func(g types.Grant) bool {
			return g.Type == types.GrantType_GRANT_TYPE_SESSION_KEY && pending(g)
		})
		if err != nil {
			return noOpErr(label, err)
		}
		for _, i := range e.r.Perm(len(sessions)) {
			g := sessions[i]
			granter, ok := e.account(g.Granter)
			if !ok {
				continue
			}
			// The facade resolves the pair through the session-key index.
			if has, err := e.k.SessionKeyByPair.Has(e.ctx, collections.Join(g.Granter, g.Grantee)); err != nil {
				return noOpErr(label, err)
			} else if !has {
				continue
			}
			return e.submit(granter, &types.MsgRevokeSession{Granter: g.Granter, Grantee: g.Grantee}, math.ZeroInt())
		}

		// Prerequisite: create a session key, then revoke it.
		for attempt := 0; attempt < 3; attempt++ {
			granter, grantee, ok := e.pair(math.NewInt(10_000))
			if !ok {
				break
			}
			created, ok, err := e.ensure(granter, e.buildCreateSession(granter, grantee), math.ZeroInt())
			if err != nil {
				return created, nil, err
			}
			if !ok {
				continue
			}
			return e.submit(granter, &types.MsgRevokeSession{Granter: granter.Address.String(), Grantee: grantee.Address.String()}, math.ZeroInt())
		}
		return noOp(label, "no live sim granter holds or can create a session key")
	})
}
