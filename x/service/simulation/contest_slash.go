package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// SimulateMsgContestSlash has an operator contest a tier-1 slash (a
// RESOLVED_T1 report whose escrow is still inside the contest window),
// which returns the escrow to its bond and escalates the report to an x/rep
// jury. A tier-1 slash takes an executed x/commons proposal of the
// controller (SimulateMsgResolveReport builds those), so there is no
// prerequisite to deliver here.
func SimulateMsgContestSlash(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgContestSlash{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		slashed, err := e.reports(types.ReportStatus_REPORT_STATUS_RESOLVED_T1, func(r types.Report) bool {
			_, ok := e.account(r.OperatorAddress)
			return ok && r.SlashAmount.IsPositive()
		})
		if err != nil {
			return noOpErr(label, err)
		}
		if len(slashed) == 0 {
			return noOp(label, "no tier-1 slash against a sim operator")
		}
		reason := ""
		for _, i := range e.r.Perm(len(slashed)) {
			rep := slashed[i]
			acc, _ := e.account(rep.OperatorAddress)
			msg := &types.MsgContestSlash{Operator: rep.OperatorAddress, ServiceType: rep.ServiceType, ReportId: rep.ReportId}
			if why := e.usable(acc, math.ZeroInt()); why != "" {
				reason = why
				continue
			}
			if err := e.dryRun(msg); err != nil {
				reason = "handler would reject: " + err.Error()
				continue
			}
			return e.deliver(acc, msg, math.ZeroInt())
		}
		return noOp(label, reason)
	})
}
