package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// SimulateMsgUpdateMetadata has a live operator (any status) replace its
// metadata blob. When no sim account is an operator one registers first.
func SimulateMsgUpdateMetadata(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgUpdateMetadata{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		op, acc, ok, err := e.signedOperator(nil)
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			return noOp(label, "no operator signed by a live sim account, and none could register")
		}
		msg := &types.MsgUpdateMetadata{
			Operator:    op.Address,
			ServiceType: op.ServiceType,
			NewMetadata: randomMetadata(e.r),
		}
		return e.submit(acc, msg, math.ZeroInt())
	})
}
