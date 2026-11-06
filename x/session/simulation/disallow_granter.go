package simulation

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// SimulateMsgDisallowGranter has a live sim grantee remove one of its
// allowed granters (consent entries are written when a grantee accepts a
// pending session key or sign-in). No-op when no live grantee allows anyone.
func SimulateMsgDisallowGranter(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	label := &types.MsgDisallowGranter{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		var entries []collections.Pair[string, string]
		if err := e.k.AllowedGranters.Walk(e.ctx, nil, func(key collections.Pair[string, string]) (bool, error) {
			entries = append(entries, key)
			return false, nil
		}); err != nil {
			return noOpErr(label, err)
		}
		for _, i := range e.r.Perm(len(entries)) {
			grantee, ok := e.account(entries[i].K1())
			if !ok {
				continue
			}
			msg := &types.MsgDisallowGranter{Grantee: entries[i].K1(), Granter: entries[i].K2()}
			if e.dryRun(msg) != nil {
				continue
			}
			return e.deliver(grantee, msg, math.ZeroInt())
		}
		return noOp(label, "no live sim grantee allows a granter")
	})
}
