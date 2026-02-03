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

func SimulateMsgSetDisplayTitle(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Ensure profile exists
		if err := getOrCreateMemberProfile(r, ctx, k, simAccount.Address.String()); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSetDisplayTitle{}), "failed to create profile"), nil, nil
		}

		// Get or create a title
		titleID, err := getOrCreateTitle(r, ctx, k)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSetDisplayTitle{}), "failed to get/create title"), nil, nil
		}

		// Unlock the title for this member (normally done through achievements)
		profile, _ := k.MemberProfile.Get(ctx, simAccount.Address.String())
		titleUnlocked := false
		for _, unlocked := range profile.UnlockedTitles {
			if unlocked == titleID {
				titleUnlocked = true
				break
			}
		}
		if !titleUnlocked {
			profile.UnlockedTitles = append(profile.UnlockedTitles, titleID)
			if err := k.MemberProfile.Set(ctx, simAccount.Address.String(), profile); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSetDisplayTitle{}), "failed to unlock title"), nil, nil
			}
		}

		msg := &types.MsgSetDisplayTitle{
			Creator: simAccount.Address.String(),
			TitleId: titleID,
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
