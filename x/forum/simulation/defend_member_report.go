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

// SimulateMsgDefendMemberReport simulates a MsgDefendMemberReport message using direct keeper calls.
// This bypasses any membership requirements for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgDefendMemberReport(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// The reported member will defend themselves
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Select a different account as the reporter
		var reporter simtypes.Account
		for {
			reporter, _ = simtypes.RandomAcc(r, accs)
			if !reporter.Address.Equals(simAccount.Address) {
				break
			}
		}

		// Get or create a member report against this account
		err := getOrCreateMemberReport(r, ctx, k, simAccount.Address.String(), reporter.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDefendMemberReport{}), "failed to get/create member report"), nil, nil
		}

		// Use direct keeper calls to submit defense (bypasses any membership check)
		report, err := k.MemberReport.Get(ctx, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDefendMemberReport{}), "report not found"), nil, nil
		}

		// Check if a defense has already been submitted
		if report.Defense != "" {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDefendMemberReport{}), "defense already submitted"), nil, nil
		}

		// Submit the defense
		report.Defense = "The report is unfounded and based on misunderstanding"
		report.DefenseSubmittedAt = ctx.BlockTime().Unix()

		if err := k.MemberReport.Set(ctx, simAccount.Address.String(), report); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDefendMemberReport{}), "failed to update report"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDefendMemberReport{}), "ok (direct keeper call)"), nil, nil
	}
}
