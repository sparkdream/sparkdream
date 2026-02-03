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

// SimulateMsgClaimGuildFounder simulates a MsgClaimGuildFounder message using direct keeper calls.
// This creates a frozen guild state first, then simulates the founder claim.
// Full integration testing should be done in integration tests.
func SimulateMsgClaimGuildFounder(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Try to find a frozen guild
		var frozenGuild *types.Guild
		var frozenGuildID uint64

		err := k.Guild.Walk(ctx, nil, func(id uint64, guild types.Guild) (bool, error) {
			if guild.Status == types.GuildStatus_GUILD_STATUS_FROZEN {
				frozenGuild = &guild
				frozenGuildID = id
				return true, nil // Stop walking
			}
			return false, nil
		})

		if err != nil || frozenGuild == nil {
			// Create a frozen guild for simulation
			guildID, err := k.GuildSeq.Next(ctx)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgClaimGuildFounder{}), "failed to get guild ID"), nil, nil
			}

			// Create a frozen guild with a different founder (so simAccount can claim it)
			otherAccount, _ := simtypes.RandomAcc(r, accs)
			if otherAccount.Address.String() == simAccount.Address.String() {
				// Pick another account
				for i := 0; i < len(accs); i++ {
					if accs[i].Address.String() != simAccount.Address.String() {
						otherAccount = accs[i]
						break
					}
				}
			}

			guild := types.Guild{
				Id:           guildID,
				Name:         randomGuildName(r),
				Description:  "A frozen guild for simulation",
				Founder:      otherAccount.Address.String(),
				InviteOnly:   false,
				CreatedBlock: ctx.BlockHeight() - 10000, // Old guild
				Status:       types.GuildStatus_GUILD_STATUS_FROZEN,
			}

			if err := k.Guild.Set(ctx, guildID, guild); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgClaimGuildFounder{}), "failed to create frozen guild"), nil, nil
			}

			frozenGuild = &guild
			frozenGuildID = guildID
		}

		// Check if simAccount is already in a guild
		existingMembership, err := k.GuildMembership.Get(ctx, simAccount.Address.String())
		if err == nil && existingMembership.GuildId != frozenGuildID {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgClaimGuildFounder{}), "already in different guild"), nil, nil
		}

		// Ensure member has a profile
		if err := getOrCreateMemberProfile(r, ctx, k, simAccount.Address.String()); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgClaimGuildFounder{}), "failed to create profile"), nil, nil
		}

		// Claim the founder position (using direct keeper call)
		frozenGuild.Founder = simAccount.Address.String()
		frozenGuild.Status = types.GuildStatus_GUILD_STATUS_ACTIVE // Unfreeze when claimed

		if err := k.Guild.Set(ctx, frozenGuildID, *frozenGuild); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgClaimGuildFounder{}), "failed to update guild"), nil, nil
		}

		// Create membership for the new founder
		membership := types.GuildMembership{
			Member:      simAccount.Address.String(),
			GuildId:     frozenGuildID,
			JoinedEpoch: 1,
		}

		if err := k.GuildMembership.Set(ctx, simAccount.Address.String(), membership); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgClaimGuildFounder{}), "failed to create membership"), nil, nil
		}

		// Update profile with guild ID
		profile, _ := k.MemberProfile.Get(ctx, simAccount.Address.String())
		profile.GuildId = frozenGuildID
		k.MemberProfile.Set(ctx, simAccount.Address.String(), profile)

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgClaimGuildFounder{}), "ok (direct keeper call)"), nil, nil
	}
}
