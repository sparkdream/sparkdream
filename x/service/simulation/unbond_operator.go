package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// SimulateMsgUnbondOperator starts the exit of an ACTIVE or UNDERFUNDED
// operator (the bond returns through MsgClaimUnbondedBond once the
// unbonding period has passed). When no sim account is such an operator one
// registers first.
func SimulateMsgUnbondOperator(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgUnbondOperator{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		op, acc, ok, err := e.signedOperator(func(op types.Operator) bool {
			return op.Status == types.OperatorStatus_OPERATOR_STATUS_ACTIVE ||
				op.Status == types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED
		})
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			return noOp(label, "no ACTIVE or UNDERFUNDED operator signed by a live sim account")
		}
		msg := &types.MsgUnbondOperator{Operator: op.Address, ServiceType: op.ServiceType}
		return e.submit(acc, msg, math.ZeroInt())
	})
}
