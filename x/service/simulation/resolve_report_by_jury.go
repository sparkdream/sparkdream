package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// SimulateMsgResolveReportByJury applies an x/rep jury verdict to an
// ESCALATED report. While the jury is undecided its seated live sim jurors
// vote first (real MsgSubmitJurorVote txs); once it has decided, a Commons
// Operations Committee member relays the matching verdict: ACCEPT (the
// proposed slash, rarely dissolving the operator) or REDUCE after an
// upheld challenge, REJECT after a rejected or inconclusive one. Reports
// reach ESCALATED through MsgResolveReport or MsgContestSlash.
func SimulateMsgResolveReportByJury(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgResolveReportByJury{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		escalated, err := e.reports(types.ReportStatus_REPORT_STATUS_ESCALATED, nil)
		if err != nil {
			return noOpErr(label, err)
		}
		rep, ok := pick(e.r, escalated)
		if !ok {
			return noOp(label, "no escalated report")
		}
		verdict, vote, why, err := e.juryVerdict(rep.JuryCaseId)
		if err != nil {
			return noOpErr(label, err)
		}
		if verdict == reptypes.Verdict_VERDICT_PENDING {
			if vote.OK {
				return relabel(vote, sdk.MsgTypeURL(label), "jurors vote on the case behind"), nil, nil
			}
			return noOp(label, "jury undecided: "+why)
		}
		relayer, ok := pick(e.r, e.verdictRelayers())
		if !ok {
			return noOp(label, "no live Commons Operations Committee member to relay the verdict")
		}
		msg := &types.MsgResolveReportByJury{JuryAuthority: relayer.Address.String(), ReportId: rep.ReportId}
		switch {
		case verdict != reptypes.Verdict_VERDICT_UPHOLD_CHALLENGE:
			msg.Verdict = types.JuryVerdict_JURY_VERDICT_REJECT
		case rep.ProposedSlashBps > 1 && e.r.Intn(2) == 0:
			msg.Verdict = types.JuryVerdict_JURY_VERDICT_REDUCE
			msg.SlashBps = 1 + e.r.Uint32()%(rep.ProposedSlashBps-1)
		default:
			msg.Verdict = types.JuryVerdict_JURY_VERDICT_ACCEPT
			msg.SlashBps = rep.ProposedSlashBps
			msg.Dissolve = e.r.Intn(10) == 0
		}
		return e.submit(relayer, msg, math.ZeroInt())
	})
}
