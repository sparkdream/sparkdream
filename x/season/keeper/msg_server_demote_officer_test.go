package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerDemoteOfficer(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.DemoteOfficer(f.ctx, &types.MsgDemoteOfficer{
			Creator: "invalid-address",
			Officer: TestAddrOfficer.String(),
			GuildId: 1,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("invalid officer address", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		_, err := ms.DemoteOfficer(ctx, &types.MsgDemoteOfficer{
			Creator: creatorStr,
			Officer: "invalid-address",
			GuildId: 1,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid officer address")
	})

	t.Run("guild not found", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		officer := TestAddrOfficer
		creatorStr, _ := f.addressCodec.BytesToString(creator)
		officerStr, _ := f.addressCodec.BytesToString(officer)

		_, err := ms.DemoteOfficer(ctx, &types.MsgDemoteOfficer{
			Creator: creatorStr,
			Officer: officerStr,
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
		officer := TestAddrOfficer
		founderStr, _ := f.addressCodec.BytesToString(founder)
		officerStr, _ := f.addressCodec.BytesToString(officer)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		// Set guild to dissolved
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.Status = types.GuildStatus_GUILD_STATUS_DISSOLVED
		k.Guild.Set(ctx, guildID, guild)

		_, err := ms.DemoteOfficer(ctx, &types.MsgDemoteOfficer{
			Creator: founderStr,
			Officer: officerStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildDissolved)
	})

	t.Run("not founder", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		officer := TestAddrOfficer
		nonFounder := TestAddrMember1
		officerStr, _ := f.addressCodec.BytesToString(officer)
		nonFounderStr, _ := f.addressCodec.BytesToString(nonFounder)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, nonFounder)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		_, err := ms.DemoteOfficer(ctx, &types.MsgDemoteOfficer{
			Creator: nonFounderStr,
			Officer: officerStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotGuildFounder)
	})

	t.Run("member not an officer", func(t *testing.T) {
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
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)
		SetupGuildFounderMembership(t, k, ctx, founder, guildID)
		AddMemberToGuild(t, k, ctx, member, guildID)

		_, err := ms.DemoteOfficer(ctx, &types.MsgDemoteOfficer{
			Creator: founderStr,
			Officer: memberStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotOfficer)
	})

	t.Run("successful demotion", func(t *testing.T) {
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
		guildID := SetupGuild(t, k, ctx, founder, "Demote Test Guild", TestGuildDesc)
		SetupGuildFounderMembership(t, k, ctx, founder, guildID)
		AddMemberToGuild(t, k, ctx, officer, guildID)

		// Make officer
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.Officers = []string{officerStr}
		k.Guild.Set(ctx, guildID, guild)

		_, err := ms.DemoteOfficer(ctx, &types.MsgDemoteOfficer{
			Creator: founderStr,
			Officer: officerStr,
			GuildId: guildID,
		})

		require.NoError(t, err)

		// Verify officer was removed from list
		guild, _ = k.Guild.Get(ctx, guildID)
		require.NotContains(t, guild.Officers, officerStr)
	})

	t.Run("demote one of multiple officers", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		officer1 := TestAddrOfficer
		officer2 := TestAddrMember1
		founderStr, _ := f.addressCodec.BytesToString(founder)
		officer1Str, _ := f.addressCodec.BytesToString(officer1)
		officer2Str, _ := f.addressCodec.BytesToString(officer2)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, officer1)
		SetupBasicMemberProfile(t, k, ctx, officer2)
		guildID := SetupGuild(t, k, ctx, founder, "Multi Officer Guild", TestGuildDesc)
		SetupGuildFounderMembership(t, k, ctx, founder, guildID)
		AddMemberToGuild(t, k, ctx, officer1, guildID)
		AddMemberToGuild(t, k, ctx, officer2, guildID)

		// Make both officers
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.Officers = []string{officer1Str, officer2Str}
		k.Guild.Set(ctx, guildID, guild)

		// Demote officer1
		_, err := ms.DemoteOfficer(ctx, &types.MsgDemoteOfficer{
			Creator: founderStr,
			Officer: officer1Str,
			GuildId: guildID,
		})

		require.NoError(t, err)

		// Verify officer1 removed but officer2 remains
		guild, _ = k.Guild.Get(ctx, guildID)
		require.NotContains(t, guild.Officers, officer1Str)
		require.Contains(t, guild.Officers, officer2Str)
	})
}
