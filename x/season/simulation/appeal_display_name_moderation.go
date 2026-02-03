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

func SimulateMsgAppealDisplayNameModeration(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Find a pending moderation case that hasn't been appealed yet
		moderation, moderatedAddr, err := findDisplayNameModeration(r, ctx, k)
		if err != nil || moderation == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAppealDisplayNameModeration{}), "no moderation cases found"), nil, nil
		}

		// Check if already appealed (AppealChallengeId is set when appealed)
		if moderation.AppealChallengeId != "" || moderation.AppealedAt > 0 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAppealDisplayNameModeration{}), "already appealed"), nil, nil
		}

		// Use the moderated user's account
		simAccount, found := getAccountForAddress(moderatedAddr, accs)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAppealDisplayNameModeration{}), "moderated user not in sim accounts"), nil, nil
		}

		msg := &types.MsgAppealDisplayNameModeration{
			Creator:      simAccount.Address.String(),
			AppealReason: "My display name was incorrectly flagged",
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
