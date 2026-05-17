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

// SimulateMsgContestSlash escalates a random RESOLVED_T1 report to
// ESCALATED via direct keeper write.
func SimulateMsgContestSlash(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		report, reportID, found := findReportWithStatus(r, ctx, k, types.ReportStatus_REPORT_STATUS_RESOLVED_T1)
		msg := &types.MsgContestSlash{}
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no resolved_t1 report"), nil, nil
		}
		msg.Operator = report.OperatorAddress
		msg.ServiceType = report.ServiceType
		msg.ReportId = reportID

		report.Status = types.ReportStatus_REPORT_STATUS_ESCALATED
		report.EscalatedAt = ctx.BlockHeight()
		_ = k.Reports.Set(ctx, reportID, report)
		_ = k.EscalatedReportsQueue.Set(ctx, collections.Join(report.EscalatedAt, reportID))

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
