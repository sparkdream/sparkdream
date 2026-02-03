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

func SimulateMsgTransferGuildFounder(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Find or create a guild where this account is the founder
		guild, guildID, err := findGuildByFounder(r, ctx, k, simAccount.Address.String())
		if err != nil || guild == nil {
			// Create a guild for this account
			guildID, err = getOrCreateGuild(r, ctx, k, simAccount.Address.String())
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTransferGuildFounder{}), "failed to get/create guild"), nil, nil
			}
		}

		// Find another member to transfer to, or pick a random account
		newFounderAccount, _ := simtypes.RandomAcc(r, accs)
		// Ensure new founder is different from current
		if newFounderAccount.Address.String() == simAccount.Address.String() {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTransferGuildFounder{}), "new founder same as current"), nil, nil
		}

		// Get guild (if not already loaded) and check if it's active
		if guild == nil {
			guildVal, err := k.Guild.Get(ctx, guildID)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTransferGuildFounder{}), "guild not found"), nil, nil
			}
			guild = &guildVal
		}
		if guild.Status != types.GuildStatus_GUILD_STATUS_ACTIVE {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTransferGuildFounder{}), "guild not active"), nil, nil
		}

		// Ensure new founder is a member of the guild (or add them)
		if err := getOrCreateGuildMember(r, ctx, k, guildID, newFounderAccount.Address.String()); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTransferGuildFounder{}), "failed to add new founder as member"), nil, nil
		}

		// Ensure new founder has a profile with the correct guild ID
		if err := getOrCreateMemberProfile(r, ctx, k, newFounderAccount.Address.String()); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTransferGuildFounder{}), "failed to create profile"), nil, nil
		}

		msg := &types.MsgTransferGuildFounder{
			Creator:    simAccount.Address.String(),
			GuildId:    guildID,
			NewFounder: newFounderAccount.Address.String(),
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
