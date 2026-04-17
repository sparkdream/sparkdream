package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerLeaveGuild(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.LeaveGuild(f.ctx, &types.MsgLeaveGuild{
			Creator: "invalid-address",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("profile not found", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		_, err := ms.LeaveGuild(ctx, &types.MsgLeaveGuild{
			Creator: creatorStr,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "profile not found")
	})

	t.Run("not in a guild", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		SetupBasicMemberProfile(t, k, ctx, creator) // GuildId = 0

		_, err := ms.LeaveGuild(ctx, &types.MsgLeaveGuild{
			Creator: creatorStr,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotInGuild)
	})

	t.Run("founder cannot leave", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		founderStr, _ := f.addressCodec.BytesToString(founder)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		// Update founder to be in guild
		profile, _ := k.MemberProfile.Get(ctx, founderStr)
		profile.GuildId = guildID
		k.MemberProfile.Set(ctx, founderStr, profile)

		_, err := ms.LeaveGuild(ctx, &types.MsgLeaveGuild{
			Creator: founderStr,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrCannotLeaveAsFounder)
	})

	t.Run("successful leave", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		member := TestAddrMember1
		memberStr, _ := f.addressCodec.BytesToString(member)

		params, _ := k.Params.Get(ctx)
		// Advance to epoch 3 so LeftEpoch will be non-zero
		ctx = ctx.WithBlockHeight(3 * params.EpochBlocks)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, member)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		// Add member to guild
		profile, _ := k.MemberProfile.Get(ctx, memberStr)
		profile.GuildId = guildID
		k.MemberProfile.Set(ctx, memberStr, profile)

		membership := types.GuildMembership{
			Member:  memberStr,
			GuildId: guildID,
		}
		k.GuildMembership.Set(ctx, memberStr, membership)
		// Counter starts at 1 to reflect the member that is about to leave.
		require.NoError(t, k.GuildMemberCount.Set(ctx, guildID, 1))

		_, err := ms.LeaveGuild(ctx, &types.MsgLeaveGuild{
			Creator: memberStr,
		})

		require.NoError(t, err)

		// Verify profile was updated
		profile, _ = k.MemberProfile.Get(ctx, memberStr)
		require.Equal(t, uint64(0), profile.GuildId)

		// Verify membership was updated
		membership, _ = k.GuildMembership.Get(ctx, memberStr)
		require.Equal(t, uint64(0), membership.GuildId)
		require.Equal(t, int64(3), membership.LeftEpoch)

		// Counter decremented on leave.
		require.Equal(t, uint64(0), k.GetGuildMemberCount(ctx, guildID))
	})

	t.Run("officer leaving is removed from officers list", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		officer := TestAddrOfficer
		officerStr, _ := f.addressCodec.BytesToString(officer)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, officer)
		guildID := SetupGuild(t, k, ctx, founder, "Guild With Officer", TestGuildDesc)

		// Add officer to guild and officers list
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.Officers = []string{officerStr}
		k.Guild.Set(ctx, guildID, guild)

		profile, _ := k.MemberProfile.Get(ctx, officerStr)
		profile.GuildId = guildID
		k.MemberProfile.Set(ctx, officerStr, profile)

		membership := types.GuildMembership{
			Member:  officerStr,
			GuildId: guildID,
		}
		k.GuildMembership.Set(ctx, officerStr, membership)

		_, err := ms.LeaveGuild(ctx, &types.MsgLeaveGuild{
			Creator: officerStr,
		})

		require.NoError(t, err)

		// Verify officer was removed from list
		guild, _ = k.Guild.Get(ctx, guildID)
		require.NotContains(t, guild.Officers, officerStr)
	})

	t.Run("guild freezes when below minimum members", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		member := TestAddrMember1
		founderStr, _ := f.addressCodec.BytesToString(founder)
		memberStr, _ := f.addressCodec.BytesToString(member)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, member)
		guildID := SetupGuild(t, k, ctx, founder, "Small Guild", TestGuildDesc)

		// Set founder as in guild
		founderProfile, _ := k.MemberProfile.Get(ctx, founderStr)
		founderProfile.GuildId = guildID
		k.MemberProfile.Set(ctx, founderStr, founderProfile)

		// Add member to guild (so we have 2 members - founder + 1)
		memberProfile, _ := k.MemberProfile.Get(ctx, memberStr)
		memberProfile.GuildId = guildID
		k.MemberProfile.Set(ctx, memberStr, memberProfile)

		membership := types.GuildMembership{
			Member:  memberStr,
			GuildId: guildID,
		}
		k.GuildMembership.Set(ctx, memberStr, membership)

		// Member leaves - now guild has 1 member (below min of 3)
		_, err := ms.LeaveGuild(ctx, &types.MsgLeaveGuild{
			Creator: memberStr,
		})

		require.NoError(t, err)

		// Verify guild was frozen
		guild, _ := k.Guild.Get(ctx, guildID)
		require.Equal(t, types.GuildStatus_GUILD_STATUS_FROZEN, guild.Status)
	})
}
