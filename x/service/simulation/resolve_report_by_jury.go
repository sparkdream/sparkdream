package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// SimulateMsgResolveReportByJury closes a random ESCALATED report
// with a REJECT verdict (no slash, no operator state change).
func SimulateMsgResolveReportByJury(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		report, reportID, found := findReportWithStatus(r, ctx, k, types.ReportStatus_REPORT_STATUS_ESCALATED)
		msg := &types.MsgResolveReportByJury{}
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no escalated report"), nil, nil
		}
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msg.JuryAuthority = simAccount.Address.String()
		msg.ReportId = reportID
		msg.Verdict = types.JuryVerdict_JURY_VERDICT_REJECT

		report.Status = types.ReportStatus_REPORT_STATUS_RESOLVED_T2
		_ = k.Reports.Set(ctx, reportID, report)
		_ = k.EscalatedReportsQueue.Remove(ctx, collections.Join(report.EscalatedAt, reportID))

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
