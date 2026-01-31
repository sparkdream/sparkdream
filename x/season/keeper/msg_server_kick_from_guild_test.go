package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerKickFromGuild(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.KickFromGuild(f.ctx, &types.MsgKickFromGuild{
			Creator: "invalid-address",
			Member:  TestAddrMember1.String(),
			GuildId: 1,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("invalid member address", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		_, err := ms.KickFromGuild(ctx, &types.MsgKickFromGuild{
			Creator: creatorStr,
			Member:  "invalid-address",
			GuildId: 1,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid member address")
	})

	t.Run("guild not found", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		member := TestAddrMember1
		creatorStr, _ := f.addressCodec.BytesToString(creator)
		memberStr, _ := f.addressCodec.BytesToString(member)

		_, err := ms.KickFromGuild(ctx, &types.MsgKickFromGuild{
			Creator: creatorStr,
			Member:  memberStr,
			GuildId: 999,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildNotFound)
	})

	t.Run("not founder or officer", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		nonOfficer := TestAddrMember1
		target := TestAddrMember2
		nonOfficerStr, _ := f.addressCodec.BytesToString(nonOfficer)
		targetStr, _ := f.addressCodec.BytesToString(target)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, nonOfficer)
		SetupBasicMemberProfile(t, k, ctx, target)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		_, err := ms.KickFromGuild(ctx, &types.MsgKickFromGuild{
			Creator: nonOfficerStr,
			Member:  targetStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotGuildFounderOrOfficer)
	})

	t.Run("cannot kick founder", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		officer := TestAddrOfficer
		founderStr, _ := f.addressCodec.BytesToString(founder)
		officerStr, _ := f.addressCodec.BytesToString(officer)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, officer)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		// Make officer
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.Officers = []string{officerStr}
		k.Guild.Set(ctx, guildID, guild)

		_, err := ms.KickFromGuild(ctx, &types.MsgKickFromGuild{
			Creator: officerStr,
			Member:  founderStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrCannotKickFounder)
	})

	t.Run("cannot kick self", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		officer := TestAddrOfficer
		officerStr, _ := f.addressCodec.BytesToString(officer)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, officer)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)
		SetupGuildFounderMembership(t, k, ctx, founder, guildID)
		AddMemberToGuild(t, k, ctx, officer, guildID)

		// Make officer
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.Officers = []string{officerStr}
		k.Guild.Set(ctx, guildID, guild)

		// Officer tries to kick themselves
		_, err := ms.KickFromGuild(ctx, &types.MsgKickFromGuild{
			Creator: officerStr,
			Member:  officerStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot kick self")
	})

	t.Run("member not in guild", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		target := TestAddrMember1
		founderStr, _ := f.addressCodec.BytesToString(founder)
		targetStr, _ := f.addressCodec.BytesToString(target)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, target) // Not in the guild
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		_, err := ms.KickFromGuild(ctx, &types.MsgKickFromGuild{
			Creator: founderStr,
			Member:  targetStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotGuildMember)
	})

	t.Run("successful kick by founder", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		member := TestAddrMember2
		founderStr, _ := f.addressCodec.BytesToString(founder)
		memberStr, _ := f.addressCodec.BytesToString(member)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, member)
		guildID := SetupGuild(t, k, ctx, founder, "Kick Test Guild", TestGuildDesc)
		SetupGuildFounderMembership(t, k, ctx, founder, guildID)
		AddMemberToGuild(t, k, ctx, member, guildID)

		_, err := ms.KickFromGuild(ctx, &types.MsgKickFromGuild{
			Creator: founderStr,
			Member:  memberStr,
			GuildId: guildID,
			Reason:  "Test kick",
		})

		require.NoError(t, err)

		// Verify member was removed
		memberProfile, _ := k.MemberProfile.Get(ctx, memberStr)
		require.Equal(t, uint64(0), memberProfile.GuildId)
	})

	t.Run("officer cannot kick other officers", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		officer1 := TestAddrOfficer
		officer2 := TestAddrMember1
		officer1Str, _ := f.addressCodec.BytesToString(officer1)
		officer2Str, _ := f.addressCodec.BytesToString(officer2)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, officer1)
		SetupBasicMemberProfile(t, k, ctx, officer2)
		guildID := SetupGuild(t, k, ctx, founder, "Officer Guild", TestGuildDesc)
		SetupGuildFounderMembership(t, k, ctx, founder, guildID)
		AddMemberToGuild(t, k, ctx, officer1, guildID)
		AddMemberToGuild(t, k, ctx, officer2, guildID)

		// Make both officers
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.Officers = []string{officer1Str, officer2Str}
		k.Guild.Set(ctx, guildID, guild)

		_, err := ms.KickFromGuild(ctx, &types.MsgKickFromGuild{
			Creator: officer1Str,
			Member:  officer2Str,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "only founder can kick officers")
	})
}
