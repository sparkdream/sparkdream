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

func SimulateMsgAcceptGuildInvite(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Check if user is already a member
		_, err := k.GuildMembership.Get(ctx, simAccount.Address.String())
		if err == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAcceptGuildInvite{}), "user already in a guild"), nil, nil
		}

		// Find or create a guild and invite
		guild, guildID, err := findGuild(r, ctx, k)
		if err != nil || guild == nil {
			// Create a guild first
			founderAccount, _ := simtypes.RandomAcc(r, accs)
			guildID, err = getOrCreateGuild(r, ctx, k, founderAccount.Address.String())
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAcceptGuildInvite{}), "failed to create guild"), nil, nil
			}
			guildVal, _ := k.Guild.Get(ctx, guildID)
			guild = &guildVal
		}

		// Check if guild is active (not frozen)
		if guild.Status != types.GuildStatus_GUILD_STATUS_ACTIVE {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAcceptGuildInvite{}), "guild not active"), nil, nil
		}

		// Ensure member has a profile
		if err := getOrCreateMemberProfile(r, ctx, k, simAccount.Address.String()); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAcceptGuildInvite{}), "failed to create profile"), nil, nil
		}

		// Create invite for simAccount
		if err := getOrCreateGuildInvite(r, ctx, k, guildID, guild.Founder, simAccount.Address.String()); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAcceptGuildInvite{}), "failed to create invite"), nil, nil
		}

		msg := &types.MsgAcceptGuildInvite{
			Creator: simAccount.Address.String(),
			GuildId: guildID,
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
