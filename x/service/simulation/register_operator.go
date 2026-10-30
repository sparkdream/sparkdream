package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// SimulateMsgRegisterOperator registers a sim account as an operator of a
// random enabled service type it holds no record under, controlled by an
// x/commons group (usually the Commons Operations Committee), with a bond
// between min_bond and twice min_bond.
func SimulateMsgRegisterOperator(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgRegisterOperator{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		controller, ok := e.controllerPolicy()
		if !ok {
			return noOp(label, "no x/commons group with members to control an operator")
		}
		reason := "no live sim account"
		for _, i := range e.r.Perm(len(e.accs)) {
			acc := e.accs[i]
			msg, bond, why, err := e.registerMsg(acc, controller)
			if err != nil {
				return noOpErr(label, err)
			}
			if why != "" {
				reason = why
				continue
			}
			if why := e.usable(acc, bond); why != "" {
				reason = why
				continue
			}
			if err := e.dryRun(msg); err != nil {
				reason = "handler would reject: " + err.Error()
				continue
			}
			return e.deliver(acc, msg, bond)
		}
		return noOp(label, reason)
	})
}
