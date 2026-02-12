package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgResolveMemberReport simulates a MsgResolveMemberReport message using direct keeper calls.
// This bypasses the operations committee requirement for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgResolveMemberReport(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Select a different account as the reported member
		var reported simtypes.Account
		for {
			reported, _ = simtypes.RandomAcc(r, accs)
			if !reported.Address.Equals(simAccount.Address) {
				break
			}
		}

		// Select another account as the reporter
		var reporter simtypes.Account
		for {
			reporter, _ = simtypes.RandomAcc(r, accs)
			if !reporter.Address.Equals(simAccount.Address) && !reporter.Address.Equals(reported.Address) {
				break
			}
		}

		// Get or create a member report
		err := getOrCreateMemberReport(r, ctx, k, reported.Address.String(), reporter.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveMemberReport{}), "failed to get/create member report"), nil, nil
		}

		// Use direct keeper calls to resolve the report (bypasses operations committee check)
		report, err := k.MemberReport.Get(ctx, reported.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveMemberReport{}), "report not found"), nil, nil
		}

		// Set status to resolved (the only available resolution status)
		report.Status = types.MemberReportStatus_MEMBER_REPORT_STATUS_RESOLVED

		if err := k.MemberReport.Set(ctx, reported.Address.String(), report); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveMemberReport{}), "failed to update report"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveMemberReport{}), "ok (direct keeper call)"), nil, nil
	}
}
