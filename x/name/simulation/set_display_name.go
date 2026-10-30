package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

// SimulateMsgSetDisplayName sets (one draw in ten: clears) a live sim
// account's display name. No membership or name is needed.
func SimulateMsgSetDisplayName(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgSetDisplayName{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		acc, ok := pick(e.r, e.live(func(a simtypes.Account) bool { return e.spendable(a).IsPositive() }))
		if !ok {
			return noOp(label, "no live sim account with SPARK for the fee")
		}
		display := randomDisplayName(e.r)
		if e.r.Intn(10) == 0 {
			display = ""
		}
		return e.submit(acc, &types.MsgSetDisplayName{Authority: acc.Address.String(), DisplayName: display}, zero())
	})
}
