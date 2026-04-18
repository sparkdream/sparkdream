package simulation

import (
	"fmt"
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func SimulateMsgCosignMemberReport(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		var reported simtypes.Account
		for {
			reported, _ = simtypes.RandomAcc(r, accs)
			if !reported.Address.Equals(simAccount.Address) {
				break
			}
		}

		var originalReporter simtypes.Account
		for {
			originalReporter, _ = simtypes.RandomAcc(r, accs)
			if !originalReporter.Address.Equals(simAccount.Address) && !originalReporter.Address.Equals(reported.Address) {
				break
			}
		}

		if err := getOrCreateMemberReport(r, ctx, k, reported.Address.String(), originalReporter.Address.String()); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCosignMemberReport{}), "failed to get/create member report"), nil, nil
		}

		report, err := k.MemberReport.Get(ctx, reported.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCosignMemberReport{}), "report not found"), nil, nil
		}

		for _, reporter := range report.Reporters {
			if reporter == simAccount.Address.String() {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCosignMemberReport{}), "already a reporter"), nil, nil
			}
		}

		report.Reporters = append(report.Reporters, simAccount.Address.String())

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

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCosignMemberReport{}), "ok (direct keeper call)"), nil, nil
	}
}
