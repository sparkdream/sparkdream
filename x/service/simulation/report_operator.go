package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// SimulateMsgReportOperator files a report against a live operator. The
// reporter meets min_reporter_trust_level, does not sit on the operator's
// controller group, is under the per-operator rate limit and refile
// cooldown, and pays the report deposit. When there is no operator one
// registers first.
func SimulateMsgReportOperator(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgReportOperator{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		ops, err := e.operators(nil)
		if err != nil {
			return noOpErr(label, err)
		}
		if len(ops) == 0 {
			op, ok, err := e.registerOperator("")
			if err != nil {
				return noOpErr(label, err)
			}
			if !ok {
				return noOp(label, "no operator, and none could register")
			}
			ops = append(ops, op)
		}
		reason := "no operator can be reported"
		for n, i := range e.r.Perm(len(ops)) {
			if n >= 5 {
				break
			}
			msg, acc, why := e.reportMsg(ops[i])
			if why != "" {
				reason = why
				continue
			}
			return e.deliver(acc, msg, e.params.ReportDepositAmount)
		}
		return noOp(label, reason)
	})
}
