package simulation

import (
	"fmt"
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgCosignMemberReport simulates a MsgCosignMemberReport message using direct keeper calls.
// This bypasses the reputation/sentinel/DREAM requirements for simulation purposes.
// Full x/rep integration testing should be done in integration tests.
func SimulateMsgCosignMemberReport(
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

		// Select another account as the original reporter (different from cosigner and reported)
		var originalReporter simtypes.Account
		for {
			originalReporter, _ = simtypes.RandomAcc(r, accs)
			if !originalReporter.Address.Equals(simAccount.Address) && !originalReporter.Address.Equals(reported.Address) {
				break
			}
		}

		// Get or create a member report
		err := getOrCreateMemberReport(r, ctx, k, reported.Address.String(), originalReporter.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCosignMemberReport{}), "failed to get/create member report"), nil, nil
		}

		// Use direct keeper calls to cosign the report (bypasses reputation/sentinel checks)
		report, err := k.MemberReport.Get(ctx, reported.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCosignMemberReport{}), "report not found"), nil, nil
		}

		// Check if already a reporter
		for _, reporter := range report.Reporters {
			if reporter == simAccount.Address.String() {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCosignMemberReport{}), "already a reporter"), nil, nil
			}
		}

		// Add cosigner to reporters list
		report.Reporters = append(report.Reporters, simAccount.Address.String())

		// Increase total bond
		currentBond, _ := math.NewIntFromString(report.TotalBond)
		if report.TotalBond == "" {
			currentBond = math.ZeroInt()
		}
		additionalBond := 10 + r.Intn(90)
		newBond := currentBond.Add(math.NewInt(int64(additionalBond)))
		report.TotalBond = fmt.Sprintf("%d", newBond.Int64())

		if err := k.MemberReport.Set(ctx, reported.Address.String(), report); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCosignMemberReport{}), "failed to update report"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCosignMemberReport{}), "ok (direct keeper call)"), nil, nil
	}
}
