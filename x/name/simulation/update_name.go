package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

// SimulateMsgUpdateName has a name's owner replace its metadata. A member
// without a name registers one first.
func SimulateMsgUpdateName(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgUpdateName{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		rec, owner, ok, err := e.ownedName(anyone, nil)
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			return noOp(label, "no live sim account owns a name and none can register one")
		}
		msg := &types.MsgUpdateName{Creator: owner.Address.String(), Name: rec.Name, Data: randomData(e.r)}
		return e.submit(owner, msg, zero())
	})
}
