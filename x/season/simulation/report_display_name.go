package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func SimulateMsgReportDisplayName(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Find someone to report
		targetAccount, _ := simtypes.RandomAcc(r, accs)
		if targetAccount.Address.String() == simAccount.Address.String() {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgReportDisplayName{}), "cannot report self"), nil, nil
		}

		// Ensure target has a profile
		if err := getOrCreateMemberProfile(r, ctx, k, targetAccount.Address.String()); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgReportDisplayName{}), "failed to create target profile"), nil, nil
		}

		// Check that target actually has a display name
		profile, err := k.MemberProfile.Get(ctx, targetAccount.Address.String())
		if err != nil || profile.DisplayName == "" {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgReportDisplayName{}), "target has no display name"), nil, nil
		}

		msg := &types.MsgReportDisplayName{
			Creator: simAccount.Address.String(),
			Target:  targetAccount.Address.String(),
			Reason:  "Inappropriate display name",
		}

		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		})
	}
}
