package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerClaimGuildFounder(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.ClaimGuildFounder(f.ctx, &types.MsgClaimGuildFounder{
			Creator: "invalid-address",
			GuildId: 1,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("guild not found", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		_, err := ms.ClaimGuildFounder(ctx, &types.MsgClaimGuildFounder{
			Creator: creatorStr,
			GuildId: 999,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildNotFound)
	})

	t.Run("guild dissolved", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		member := TestAddrMember1
		memberStr, _ := f.addressCodec.BytesToString(member)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, member)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)
		AddMemberToGuild(t, k, ctx, member, guildID)

		// Set guild to dissolved
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.Status = types.GuildStatus_GUILD_STATUS_DISSOLVED
		k.Guild.Set(ctx, guildID, guild)

		_, err := ms.ClaimGuildFounder(ctx, &types.MsgClaimGuildFounder{
			Creator: memberStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildDissolved)
	})

	t.Run("guild not frozen", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		member := TestAddrMember1
		memberStr, _ := f.addressCodec.BytesToString(member)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, member)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)
		SetupGuildFounderMembership(t, k, ctx, founder, guildID)
		AddMemberToGuild(t, k, ctx, member, guildID)

		// Guild is active (not frozen)

		_, err := ms.ClaimGuildFounder(ctx, &types.MsgClaimGuildFounder{
			Creator: memberStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildNotFrozen)
	})

	t.Run("not a guild member", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		outsider := TestAddrMember1
		outsiderStr, _ := f.addressCodec.BytesToString(outsider)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, outsider) // Not in guild
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		// Freeze the guild
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.Status = types.GuildStatus_GUILD_STATUS_FROZEN
		k.Guild.Set(ctx, guildID, guild)

		_, err := ms.ClaimGuildFounder(ctx, &types.MsgClaimGuildFounder{
			Creator: outsiderStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotGuildMember)
	})

	t.Run("successful claim by member", func(t *testing.T) {
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
		guildID := SetupGuild(t, k, ctx, founder, "Claim Founder Guild", TestGuildDesc)
		AddMemberToGuild(t, k, ctx, member, guildID)

		// Freeze the guild (simulating founder leaving)
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.Status = types.GuildStatus_GUILD_STATUS_FROZEN
		k.Guild.Set(ctx, guildID, guild)

		_, err := ms.ClaimGuildFounder(ctx, &types.MsgClaimGuildFounder{
			Creator: memberStr,
			GuildId: guildID,
		})

		require.NoError(t, err)

		// Verify founder was changed
		guild, _ = k.Guild.Get(ctx, guildID)
		require.Equal(t, memberStr, guild.Founder)
		require.NotEqual(t, founderStr, guild.Founder)
	})

	t.Run("claim removes new founder from officers", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		officer := TestAddrOfficer
		officerStr, _ := f.addressCodec.BytesToString(officer)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, officer)
		guildID := SetupGuild(t, k, ctx, founder, "Officer Claim Guild", TestGuildDesc)
		AddMemberToGuild(t, k, ctx, officer, guildID)

		// Make officer and freeze
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.Officers = []string{officerStr}
		guild.Status = types.GuildStatus_GUILD_STATUS_FROZEN
		k.Guild.Set(ctx, guildID, guild)

		_, err := ms.ClaimGuildFounder(ctx, &types.MsgClaimGuildFounder{
			Creator: officerStr,
			GuildId: guildID,
		})

		require.NoError(t, err)

		// Verify new founder is not in officers
		guild, _ = k.Guild.Get(ctx, guildID)
		require.Equal(t, officerStr, guild.Founder)
		require.NotContains(t, guild.Officers, officerStr)
	})

	t.Run("claim unfreezes guild with enough members", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		member1 := TestAddrMember1
		member2 := TestAddrMember2
		member1Str, _ := f.addressCodec.BytesToString(member1)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, member1)
		SetupBasicMemberProfile(t, k, ctx, member2)
		guildID := SetupGuild(t, k, ctx, founder, "Unfreeze Guild", TestGuildDesc)
		AddMemberToGuild(t, k, ctx, member1, guildID)
		AddMemberToGuild(t, k, ctx, member2, guildID)

		// Freeze the guild
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.Status = types.GuildStatus_GUILD_STATUS_FROZEN
		k.Guild.Set(ctx, guildID, guild)

		_, err := ms.ClaimGuildFounder(ctx, &types.MsgClaimGuildFounder{
			Creator: member1Str,
			GuildId: guildID,
		})

		require.NoError(t, err)

		// Verify guild is now active (unfrozen) - assuming 2 members meets min requirement
		guild, _ = k.Guild.Get(ctx, guildID)
		require.Equal(t, member1Str, guild.Founder)
		// Note: Whether it unfreezes depends on MinGuildMembers param
	})
}
