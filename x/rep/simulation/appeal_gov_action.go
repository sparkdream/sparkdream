package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgAppealGovAction has a member warned or demoted by a resolved
// report appeal the action within the appeal window, paying the SPARK
// appeal bond; an x/rep jury is seated on the appeal.
func SimulateMsgAppealGovAction(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgAppealGovAction{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		now := e.ctx.BlockTime().Unix()
		reps, err := e.memberReports(func(r types.MemberReport) bool {
			_, ok := e.account(r.Member)
			if !ok || r.Status != types.MemberReportStatus_MEMBER_REPORT_STATUS_RESOLVED ||
				!types.MemberActionAppealTypes[r.ResolvedAction] || now > r.ResolvedAt+types.DefaultAppealDeadline {
				return false
			}
			rest, err := e.k.MemberActionRestorations.Get(e.ctx, r.Member)
			return err == nil && !rest.Appealed && rest.ResolvedAt == r.ResolvedAt
		})
		if err != nil {
			return noOpErr(label, err)
		}
		bond := math.NewInt(types.DefaultAppealBondAmount)
		cands := make([]candidate, 0, len(reps))
		for _, rep := range reps {
			acc, _ := e.account(rep.Member)
			cands = append(cands, candidate{acc: acc, msg: &types.MsgAppealGovAction{
				Creator: rep.Member, ActionType: commontypes.Saturate[uint64](rep.ResolvedAction), ActionTarget: rep.Member, AppealReason: randomReason(e.r),
			}, spent: bond})
		}
		return e.firstValid(label, cands, 4, "no live member with an appealable warning or demotion")
	})
}
