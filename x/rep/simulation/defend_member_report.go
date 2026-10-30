package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgDefendMemberReport has a reported member file its defense on a
// PENDING or ESCALATED report.
func SimulateMsgDefendMemberReport(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgDefendMemberReport{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		reps, err := e.memberReports(func(r types.MemberReport) bool {
			_, ok := e.account(r.Member)
			return ok && r.Defense == "" && (r.Status == types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING ||
				r.Status == types.MemberReportStatus_MEMBER_REPORT_STATUS_ESCALATED)
		})
		if err != nil {
			return noOpErr(label, err)
		}
		cands := make([]candidate, 0, len(reps))
		for _, rep := range reps {
			acc, _ := e.account(rep.Member)
			cands = append(cands, candidate{acc: acc, msg: &types.MsgDefendMemberReport{
				Creator: rep.Member, Defense: randomReason(e.r),
			}, spent: math.ZeroInt()})
		}
		return e.firstValid(label, cands, 4, "no open report on a live member without a defense")
	})
}
