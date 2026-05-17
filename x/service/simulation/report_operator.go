package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// SimulateMsgReportOperator writes a PENDING report row against a
// random live operator. Reporter is a random sim account.
func SimulateMsgReportOperator(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		op, found := findRandomOperator(r, ctx, k)
		msg := &types.MsgReportOperator{}
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no operator available"), nil, nil
		}
		reporter, _ := simtypes.RandomAcc(r, accs)
		msg.Reporter = reporter.Address.String()
		msg.Operator = op.Address
		msg.ServiceType = op.ServiceType
		msg.Reason = "sim-report"

		params, err := k.Params.Get(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to load params"), nil, nil
		}

		reportID, err := k.NextReportID.Next(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to allocate report id"), nil, nil
		}
		if reportID == 0 {
			reportID, err = k.NextReportID.Next(ctx)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to allocate report id"), nil, nil
			}
		}

		height := ctx.BlockHeight()
		report := types.Report{
			ReportId:        reportID,
			OperatorAddress: op.Address,
			ServiceType:     op.ServiceType,
			Reporter:        reporter.Address.String(),
			Reason:          msg.Reason,
			FiledAt:         height,
			Status:          types.ReportStatus_REPORT_STATUS_PENDING,
			SlashAmount:     sdk.NewCoin(types.BondDenom, sdkmath.ZeroInt()),
			Deposit:         params.ReportDeposit,
		}
		if err := k.Reports.Set(ctx, reportID, report); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to write report"), nil, nil
		}
		opBytes := sdk.AccAddress(op.Address).Bytes()
		if accBytes, decodeErr := sdk.AccAddressFromBech32(op.Address); decodeErr == nil {
			opBytes = accBytes.Bytes()
		}
		_ = k.ReportsByOperator.Set(ctx, collections.Join3(opBytes, op.ServiceType, reportID))
		_ = k.PendingReportsQueue.Set(ctx, collections.Join(height, reportID))

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
