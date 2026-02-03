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

// SimulateMsgKickFromGuild simulates a MsgKickFromGuild message using direct keeper calls.
// This bypasses the founder/officer permission check for simulation purposes.
// Full permission testing should be done in integration tests.
func SimulateMsgKickFromGuild(
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
			guildID, err = getOrCreateGuild(r, ctx, k, simAccount.Address.String())
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgKickFromGuild{}), "failed to get/create guild"), nil, nil
			}
			guildVal, _ := k.Guild.Get(ctx, guildID)
			guild = &guildVal
		}

		// Check if guild is active, if not make it active for simulation
		if guild.Status != types.GuildStatus_GUILD_STATUS_ACTIVE {
			guild.Status = types.GuildStatus_GUILD_STATUS_ACTIVE
			if err := k.Guild.Set(ctx, guildID, *guild); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgKickFromGuild{}), "failed to activate guild"), nil, nil
			}
		}

		// Find a member to kick (excluding the founder)
		var memberAddr string
		membership, foundAddr, err := findGuildMemberByGuild(r, ctx, k, guildID)
		if err == nil && membership != nil && foundAddr != simAccount.Address.String() {
			memberAddr = foundAddr
		} else {
			// Need to add a new member to kick
			// Try multiple accounts to find one not already in another guild
			var memberAccount simtypes.Account
			foundMember := false
			for i := 0; i < len(accs) && !foundMember; i++ {
				candidate, _ := simtypes.RandomAcc(r, accs)
				if candidate.Address.String() == simAccount.Address.String() {
					continue // Skip self
				}
				// Check if candidate is already in a different guild
				existingMembership, mErr := k.GuildMembership.Get(ctx, candidate.Address.String())
				if mErr != nil {
					// Not in any guild - good candidate
					memberAccount = candidate
					foundMember = true
				} else if existingMembership.GuildId == guildID {
					// Already in this guild - can use them
					memberAddr = candidate.Address.String()
					foundMember = true
					break
				}
				// Otherwise, in different guild - try next
			}
			if memberAddr == "" && foundMember {
				// Add the new member
				if err := getOrCreateGuildMember(r, ctx, k, guildID, memberAccount.Address.String()); err != nil {
					return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgKickFromGuild{}), "failed to add member"), nil, nil
				}
				memberAddr = memberAccount.Address.String()
			} else if memberAddr == "" {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgKickFromGuild{}), "no suitable member found"), nil, nil
			}
		}

		// Use direct keeper calls to simulate kick (bypasses permission checks)

		// Remove the membership
		if err := k.GuildMembership.Remove(ctx, memberAddr); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgKickFromGuild{}), "failed to remove membership"), nil, nil
		}

		// Update the member's profile to clear GuildId
		profile, err := k.MemberProfile.Get(ctx, memberAddr)
		if err == nil && profile.GuildId == guildID {
			profile.GuildId = 0
			k.MemberProfile.Set(ctx, memberAddr, profile)
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgKickFromGuild{}), "ok (direct keeper call)"), nil, nil
	}
}
