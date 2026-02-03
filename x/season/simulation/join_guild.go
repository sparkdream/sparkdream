package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgJoinGuild simulates a MsgJoinGuild message using direct keeper calls.
// This bypasses the maintenance mode check for simulation purposes.
// Full maintenance mode testing should be done in integration tests.
func SimulateMsgJoinGuild(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Find an open (non-invite-only) guild
		guild, guildID, err := findOpenGuild(r, ctx, k)
		if err != nil || guild == nil {
			// Create a guild first
			otherAccount, _ := simtypes.RandomAcc(r, accs)
			guildID, err = getOrCreateGuild(r, ctx, k, otherAccount.Address.String())
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgJoinGuild{}), "failed to get/create guild"), nil, nil
			}
			// Load the guild to check status
			guildVal, _ := k.Guild.Get(ctx, guildID)
			guild = &guildVal
		}

		// Check if guild is active, if not make it active for simulation
		if guild.Status != types.GuildStatus_GUILD_STATUS_ACTIVE {
			guild.Status = types.GuildStatus_GUILD_STATUS_ACTIVE
			guild.InviteOnly = false // Ensure it's open
			if err := k.Guild.Set(ctx, guildID, *guild); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgJoinGuild{}), "failed to activate guild"), nil, nil
			}
		}

		// Make sure guild is not invite-only
		if guild.InviteOnly {
			guild.InviteOnly = false
			if err := k.Guild.Set(ctx, guildID, *guild); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgJoinGuild{}), "failed to make guild open"), nil, nil
			}
		}

		// Check if user is already a member of any guild
		_, err = k.GuildMembership.Get(ctx, simAccount.Address.String())
		if err == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgJoinGuild{}), "user already in a guild"), nil, nil
		}

		// Ensure member has a profile
		if err := getOrCreateMemberProfile(r, ctx, k, simAccount.Address.String()); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgJoinGuild{}), "failed to create profile"), nil, nil
		}

		// Use direct keeper calls to join (bypasses maintenance mode check)

		// Create membership
		membership := types.GuildMembership{
			Member:      simAccount.Address.String(),
			GuildId:     guildID,
			JoinedEpoch: 1, // Default epoch
		}

		if err := k.GuildMembership.Set(ctx, simAccount.Address.String(), membership); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgJoinGuild{}), "failed to create membership"), nil, nil
		}

		// Update member's profile with GuildId
		profile, err := k.MemberProfile.Get(ctx, simAccount.Address.String())
		if err == nil {
			profile.GuildId = guildID
			k.MemberProfile.Set(ctx, simAccount.Address.String(), profile)
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgJoinGuild{}), "ok (direct keeper call)"), nil, nil
	}
}
