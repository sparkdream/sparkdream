package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgCosignMemberReport has another eligible reporter co-sign a
// PENDING member report (the cosign threshold escalates it).
func SimulateMsgCosignMemberReport(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgCosignMemberReport{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		reps, err := e.memberReports(func(r types.MemberReport) bool {
			return r.Status == types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING
		})
		if err != nil {
			return noOpErr(label, err)
		}
		if len(reps) == 0 {
			return noOp(label, "no PENDING member report")
		}
		var cands []candidate
		for _, i := range e.r.Perm(len(reps)) {
			rep := reps[i]
			signed := map[string]bool{rep.Member: true}
			for _, s := range rep.Reporters {
				signed[s] = true
			}
			rs, err := e.reporters(signed)
			if err != nil {
				return noOpErr(label, err)
			}
			for _, ma := range rs {
				if e.k.SameMember(e.ctx, ma.addr(), rep.Member) {
					continue
				}
				cands = append(cands, candidate{acc: ma.acc, msg: &types.MsgCosignMemberReport{
					Creator: ma.addr(), Member: rep.Member,
				}, spent: math.ZeroInt()})
			}
			if len(cands) > 0 {
				break
			}
		}
		return e.firstValid(label, cands, 4, "no eligible co-signer for a PENDING report")
	})
}
