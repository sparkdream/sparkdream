package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func SimulateMsgDefendMemberReport(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		var reporter simtypes.Account
		for {
			reporter, _ = simtypes.RandomAcc(r, accs)
			if !reporter.Address.Equals(simAccount.Address) {
				break
			}
		}

		if err := getOrCreateMemberReport(r, ctx, k, simAccount.Address.String(), reporter.Address.String()); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDefendMemberReport{}), "failed to get/create member report"), nil, nil
		}

		report, err := k.MemberReport.Get(ctx, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDefendMemberReport{}), "report not found"), nil, nil
		}

		if report.Defense != "" {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDefendMemberReport{}), "defense already submitted"), nil, nil
		}

		report.Defense = "The report is unfounded and based on misunderstanding"
		report.DefenseSubmittedAt = ctx.BlockTime().Unix()

		if err := k.MemberReport.Set(ctx, simAccount.Address.String(), report); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDefendMemberReport{}), "failed to update report"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDefendMemberReport{}), "ok (direct keeper call)"), nil, nil
	}
}
