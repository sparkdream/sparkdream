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

// SimulateMsgAcceptGuildInvite simulates a MsgAcceptGuildInvite message using direct keeper calls.
// This bypasses the maintenance mode check for simulation purposes.
// Full maintenance mode testing should be done in integration tests.
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
			founderAccount, _ := simtypes.RandomAcc(r, accs)
			guildID, err = getOrCreateGuild(r, ctx, k, founderAccount.Address.String())
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAcceptGuildInvite{}), "failed to create guild"), nil, nil
			}
			guildVal, _ := k.Guild.Get(ctx, guildID)
			guild = &guildVal
		}

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

		// Use direct keeper calls to accept invite (bypasses maintenance mode check)

		// Create membership
		membership := types.GuildMembership{
			Member:      simAccount.Address.String(),
			GuildId:     guildID,
			JoinedEpoch: 1,
		}

		if err := k.GuildMembership.Set(ctx, simAccount.Address.String(), membership); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAcceptGuildInvite{}), "failed to create membership"), nil, nil
		}

		// Update profile with guild ID
		profile, err := k.MemberProfile.Get(ctx, simAccount.Address.String())
		if err == nil {
			profile.GuildId = guildID
			k.MemberProfile.Set(ctx, simAccount.Address.String(), profile)
		}

		// Remove the invite
		k.GuildInvite.Remove(ctx, simAccount.Address.String())

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAcceptGuildInvite{}), "ok (direct keeper call)"), nil, nil
	}
}
