package simulation

import (
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgReportMember simulates a MsgReportMember message using direct keeper calls.
// This bypasses the reputation/sentinel requirements for simulation purposes.
func SimulateMsgReportMember(
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

		now := ctx.BlockTime().Unix()
		report := types.MemberReport{
			Member:    reported.Address.String(),
			Reason:    "Simulation test report",
			Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
			CreatedAt: now,
			Reporters: []string{simAccount.Address.String()},
			TotalBond: fmt.Sprintf("%d", 100+r.Intn(400)),
		}

		if err := k.MemberReport.Set(ctx, reported.Address.String(), report); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgReportMember{}), "failed to create report"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgReportMember{}), "ok (direct keeper call)"), nil, nil
	}
}
