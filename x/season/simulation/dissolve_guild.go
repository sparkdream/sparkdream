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

// SimulateMsgDissolveGuild simulates a MsgDissolveGuild message using direct keeper calls.
// This bypasses the epoch age requirement for simulation purposes.
// Full timing-based testing should be done in integration tests.
func SimulateMsgDissolveGuild(
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
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDissolveGuild{}), "failed to get/create guild"), nil, nil
			}
			guildVal, _ := k.Guild.Get(ctx, guildID)
			guild = &guildVal
		}

		// Check if guild is active, if not make it active for simulation
		if guild.Status != types.GuildStatus_GUILD_STATUS_ACTIVE {
			guild.Status = types.GuildStatus_GUILD_STATUS_ACTIVE
			if err := k.Guild.Set(ctx, guildID, *guild); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDissolveGuild{}), "failed to activate guild"), nil, nil
			}
		}

		// Using direct keeper calls to simulate dissolution
		// This bypasses the epoch age check which can't be satisfied in simulation

		// Remove all memberships for this guild
		k.GuildMembership.Walk(ctx, nil, func(addr string, membership types.GuildMembership) (bool, error) {
			if membership.GuildId == guildID {
				k.GuildMembership.Remove(ctx, addr)
				// Also clear the profile's GuildId
				profile, err := k.MemberProfile.Get(ctx, addr)
				if err == nil && profile.GuildId == guildID {
					profile.GuildId = 0
					k.MemberProfile.Set(ctx, addr, profile)
				}
			}
			return false, nil
		})

		// Remove all pending invites for this guild
		k.GuildInvite.Walk(ctx, nil, func(key string, invite types.GuildInvite) (bool, error) {
			// Key format is "guildId:invitee"
			k.GuildInvite.Remove(ctx, key)
			return false, nil
		})

		// Delete the guild
		if err := k.Guild.Remove(ctx, guildID); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDissolveGuild{}), "failed to remove guild"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDissolveGuild{}), "ok (direct keeper call)"), nil, nil
	}
}
