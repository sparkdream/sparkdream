package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgResolveMemberReport resolves an open member report through an
// executed x/commons proposal of the Commons Operations Committee
// (council.go): a dismissal, a warning or now and then a demotion. Zeroing
// is not simulated (it would retire a sim member for the rest of the run).
func SimulateMsgResolveMemberReport(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgResolveMemberReport{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		bodies := e.groupsAllowing(sdk.MsgTypeURL(label))
		if len(bodies) == 0 {
			return noOp(label, "no x/commons group may execute MsgResolveMemberReport")
		}
		return e.advance(label, bodies, decoder(func() *types.MsgResolveMemberReport { return &types.MsgResolveMemberReport{} }),
			func(policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
				covered := map[string]bool{}
				for _, m := range taken {
					if t, ok := m.(*types.MsgResolveMemberReport); ok {
						covered[t.Member] = true
					}
				}
				reps, err := e.memberReports(func(r types.MemberReport) bool {
					return !covered[r.Member] && (r.Status == types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING ||
						r.Status == types.MemberReportStatus_MEMBER_REPORT_STATUS_ESCALATED)
				})
				if err != nil {
					return nil, "", err
				}
				for _, i := range e.r.Perm(len(reps)) {
					action := types.GovActionType_GOV_ACTION_TYPE_UNSPECIFIED
					switch x := e.r.Intn(10); {
					case x < 5:
						action = types.GovActionType_GOV_ACTION_TYPE_WARNING
					case x < 6:
						action = types.GovActionType_GOV_ACTION_TYPE_DEMOTION
					}
					msg := &types.MsgResolveMemberReport{
						Creator: policy, Member: reps[i].Member, Action: commontypes.Saturate[uint64](action), Reason: randomReason(e.r),
					}
					if e.dryRun(msg) == nil {
						return msg, "", nil
					}
				}
				return nil, "no open member report this body may resolve", nil
			})
	})
}
