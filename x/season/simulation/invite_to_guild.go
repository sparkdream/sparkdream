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

func SimulateMsgInviteToGuild(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Find or create a guild where this account is the founder (or officer)
		guild, guildID, err := findGuildByFounder(r, ctx, k, simAccount.Address.String())
		if err != nil || guild == nil {
			guildID, err = getOrCreateGuild(r, ctx, k, simAccount.Address.String())
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgInviteToGuild{}), "failed to get/create guild"), nil, nil
			}
			// Load the guild to check status
			guildVal, _ := k.Guild.Get(ctx, guildID)
			guild = &guildVal
		}

		// Check if guild is active
		if guild.Status != types.GuildStatus_GUILD_STATUS_ACTIVE {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgInviteToGuild{}), "guild not active"), nil, nil
		}

		// Find someone to invite
		inviteeAccount, _ := simtypes.RandomAcc(r, accs)
		if inviteeAccount.Address.String() == simAccount.Address.String() {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgInviteToGuild{}), "cannot invite self"), nil, nil
		}

		// Check if they're already a member
		_, err = k.GuildMembership.Get(ctx, inviteeAccount.Address.String())
		if err == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgInviteToGuild{}), "invitee already in a guild"), nil, nil
		}

		// Ensure invitee has a profile
		if err := getOrCreateMemberProfile(r, ctx, k, inviteeAccount.Address.String()); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgInviteToGuild{}), "failed to create invitee profile"), nil, nil
		}

		msg := &types.MsgInviteToGuild{
			Creator: simAccount.Address.String(),
			GuildId: guildID,
			Invitee: inviteeAccount.Address.String(),
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
