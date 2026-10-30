package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgReportMember has a member with the reporter reputation tier and
// sentinel bond report another member (never an x/commons seat holder),
// recommending a warning or a demotion.
func SimulateMsgReportMember(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgReportMember{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		rs, err := e.reporters(nil)
		if err != nil {
			return noOpErr(label, err)
		}
		if len(rs) == 0 {
			return noOp(label, "no member has the reputation tier and sentinel bond to report")
		}
		var addrs []string
		if err := e.k.Member.Walk(e.ctx, nil, func(addr string, m types.Member) (bool, error) {
			if m.Status == types.MemberStatus_MEMBER_STATUS_ACTIVE && !e.k.HasOpenMemberReport(e.ctx, addr) && !e.seated(addr) {
				addrs = append(addrs, addr)
			}
			return false, nil
		}); err != nil {
			return noOpErr(label, err)
		}
		var cands []candidate
		for _, ma := range rs {
			target, ok := pick(e.r, addrs)
			if !ok || e.k.SameMember(e.ctx, target, ma.addr()) {
				continue
			}
			action := types.GovActionType_GOV_ACTION_TYPE_WARNING
			if e.r.Intn(4) == 0 {
				action = types.GovActionType_GOV_ACTION_TYPE_DEMOTION
			}
			cands = append(cands, candidate{acc: ma.acc, msg: &types.MsgReportMember{
				Creator: ma.addr(), Member: target, Reason: randomReason(e.r), RecommendedAction: commontypes.Saturate[uint64](action),
			}, spent: math.ZeroInt()})
		}
		return e.firstValid(label, cands, 4, "no reportable member")
	})
}
