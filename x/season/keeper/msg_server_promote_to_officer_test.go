package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerPromoteToOfficer(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.PromoteToOfficer(f.ctx, &types.MsgPromoteToOfficer{
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

		_, err := ms.PromoteToOfficer(ctx, &types.MsgPromoteToOfficer{
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

		_, err := ms.PromoteToOfficer(ctx, &types.MsgPromoteToOfficer{
			Creator: creatorStr,
			Member:  memberStr,
			GuildId: 999,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildNotFound)
	})

	t.Run("not founder", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		nonFounder := TestAddrMember1
		target := TestAddrMember2
		nonFounderStr, _ := f.addressCodec.BytesToString(nonFounder)
		targetStr, _ := f.addressCodec.BytesToString(target)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		_, err := ms.PromoteToOfficer(ctx, &types.MsgPromoteToOfficer{
			Creator: nonFounderStr,
			Member:  targetStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotGuildFounder)
	})

	t.Run("cannot promote self", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		founderStr, _ := f.addressCodec.BytesToString(founder)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		_, err := ms.PromoteToOfficer(ctx, &types.MsgPromoteToOfficer{
			Creator: founderStr,
			Member:  founderStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrFounderCannotBeOfficer)
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
		SetupBasicMemberProfile(t, k, ctx, target) // Not in guild
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		_, err := ms.PromoteToOfficer(ctx, &types.MsgPromoteToOfficer{
			Creator: founderStr,
			Member:  targetStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotGuildMember)
	})

	t.Run("already an officer", func(t *testing.T) {
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
		SetupGuildFounderMembership(t, k, ctx, founder, guildID)
		AddMemberToGuild(t, k, ctx, officer, guildID)

		// Add officer to officers list
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.Officers = []string{officerStr}
		k.Guild.Set(ctx, guildID, guild)

		_, err := ms.PromoteToOfficer(ctx, &types.MsgPromoteToOfficer{
			Creator: founderStr,
			Member:  officerStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrAlreadyOfficer)
	})

	t.Run("successful promotion", func(t *testing.T) {
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
		guildID := SetupGuild(t, k, ctx, founder, "Promotion Guild", TestGuildDesc)
		SetupGuildFounderMembership(t, k, ctx, founder, guildID)
		AddMemberToGuild(t, k, ctx, member, guildID)

		_, err := ms.PromoteToOfficer(ctx, &types.MsgPromoteToOfficer{
			Creator: founderStr,
			Member:  memberStr,
			GuildId: guildID,
		})

		require.NoError(t, err)

		// Verify member is now an officer
		guild, _ := k.Guild.Get(ctx, guildID)
		require.Contains(t, guild.Officers, memberStr)
	})

	t.Run("max officers reached", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		newMember := TestAddrMember3
		founderStr, _ := f.addressCodec.BytesToString(founder)
		newMemberStr, _ := f.addressCodec.BytesToString(newMember)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, newMember)
		guildID := SetupGuild(t, k, ctx, founder, "Full Officers Guild", TestGuildDesc)
		SetupGuildFounderMembership(t, k, ctx, founder, guildID)
		AddMemberToGuild(t, k, ctx, newMember, guildID)

		params, _ := k.Params.Get(ctx)

		// Fill up officers list with fake addresses
		guild, _ := k.Guild.Get(ctx, guildID)
		for i := uint32(0); i < params.MaxGuildOfficers; i++ {
			guild.Officers = append(guild.Officers, "officer"+string(rune('0'+i)))
		}
		k.Guild.Set(ctx, guildID, guild)

		_, err := ms.PromoteToOfficer(ctx, &types.MsgPromoteToOfficer{
			Creator: founderStr,
			Member:  newMemberStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrMaxOfficers)
	})
}
