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

// SimulateMsgResolveReport closes a random PENDING report with a
// T1_DISMISS verdict (the simplest path — no slash, no escrow side
// effects). The msg.Controller field is filled in for shape only; the
// keeper write bypasses signer checks.
func SimulateMsgResolveReport(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		report, reportID, found := findReportWithStatus(r, ctx, k, types.ReportStatus_REPORT_STATUS_PENDING)
		msg := &types.MsgResolveReport{}
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no pending report"), nil, nil
		}
		op, ok := k.GetOperator(ctx, mustAccBytes(report.OperatorAddress), report.ServiceType)
		if !ok {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "operator missing"), nil, nil
		}
		msg.Controller = op.Controller
		msg.ReportId = reportID
		msg.Verdict = types.ResolveVerdict_RESOLVE_VERDICT_T1_DISMISS

		report.Status = types.ReportStatus_REPORT_STATUS_RESOLVED_T1
		_ = k.Reports.Set(ctx, reportID, report)
		_ = k.PendingReportsQueue.Remove(ctx, collections.Join(report.FiledAt, reportID))

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}

// mustAccBytes returns the raw bytes of a bech32 address, or nil if
// decoding fails. The keeper's typed reads ignore the nil case.
func mustAccBytes(addr string) []byte {
	acc, err := sdk.AccAddressFromBech32(addr)
	if err != nil {
		return nil
	}
	return acc.Bytes()
}
