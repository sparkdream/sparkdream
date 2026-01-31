package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerTransferGuildFounder(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.TransferGuildFounder(f.ctx, &types.MsgTransferGuildFounder{
			Creator:    "invalid-address",
			NewFounder: TestAddrMember1.String(),
			GuildId:    1,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("invalid new founder address", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		_, err := ms.TransferGuildFounder(ctx, &types.MsgTransferGuildFounder{
			Creator:    creatorStr,
			NewFounder: "invalid-address",
			GuildId:    1,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid new founder address")
	})

	t.Run("guild not found", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		newFounder := TestAddrMember1
		creatorStr, _ := f.addressCodec.BytesToString(creator)
		newFounderStr, _ := f.addressCodec.BytesToString(newFounder)

		_, err := ms.TransferGuildFounder(ctx, &types.MsgTransferGuildFounder{
			Creator:    creatorStr,
			NewFounder: newFounderStr,
			GuildId:    999,
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
		newFounder := TestAddrMember1
		founderStr, _ := f.addressCodec.BytesToString(founder)
		newFounderStr, _ := f.addressCodec.BytesToString(newFounder)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		// Set guild to dissolved
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.Status = types.GuildStatus_GUILD_STATUS_DISSOLVED
		k.Guild.Set(ctx, guildID, guild)

		_, err := ms.TransferGuildFounder(ctx, &types.MsgTransferGuildFounder{
			Creator:    founderStr,
			NewFounder: newFounderStr,
			GuildId:    guildID,
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
		nonFounder := TestAddrMember1
		newFounder := TestAddrMember2
		nonFounderStr, _ := f.addressCodec.BytesToString(nonFounder)
		newFounderStr, _ := f.addressCodec.BytesToString(newFounder)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, nonFounder)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		_, err := ms.TransferGuildFounder(ctx, &types.MsgTransferGuildFounder{
			Creator:    nonFounderStr,
			NewFounder: newFounderStr,
			GuildId:    guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotGuildFounder)
	})

	t.Run("new founder not a member", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		newFounder := TestAddrMember1
		founderStr, _ := f.addressCodec.BytesToString(founder)
		newFounderStr, _ := f.addressCodec.BytesToString(newFounder)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, newFounder) // Not in guild
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)
		SetupGuildFounderMembership(t, k, ctx, founder, guildID)

		_, err := ms.TransferGuildFounder(ctx, &types.MsgTransferGuildFounder{
			Creator:    founderStr,
			NewFounder: newFounderStr,
			GuildId:    guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotGuildMember)
	})

	t.Run("cannot transfer to self", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		founderStr, _ := f.addressCodec.BytesToString(founder)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)
		SetupGuildFounderMembership(t, k, ctx, founder, guildID)

		_, err := ms.TransferGuildFounder(ctx, &types.MsgTransferGuildFounder{
			Creator:    founderStr,
			NewFounder: founderStr,
			GuildId:    guildID,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot transfer to self")
	})

	t.Run("successful transfer", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		newFounder := TestAddrMember1
		founderStr, _ := f.addressCodec.BytesToString(founder)
		newFounderStr, _ := f.addressCodec.BytesToString(newFounder)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, newFounder)
		guildID := SetupGuild(t, k, ctx, founder, "Transfer Test Guild", TestGuildDesc)
		SetupGuildFounderMembership(t, k, ctx, founder, guildID)
		AddMemberToGuild(t, k, ctx, newFounder, guildID)

		_, err := ms.TransferGuildFounder(ctx, &types.MsgTransferGuildFounder{
			Creator:    founderStr,
			NewFounder: newFounderStr,
			GuildId:    guildID,
		})

		require.NoError(t, err)

		// Verify founder was changed
		guild, _ := k.Guild.Get(ctx, guildID)
		require.Equal(t, newFounderStr, guild.Founder)
	})

	t.Run("transfer removes new founder from officers", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		newFounder := TestAddrOfficer
		founderStr, _ := f.addressCodec.BytesToString(founder)
		newFounderStr, _ := f.addressCodec.BytesToString(newFounder)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, newFounder)
		guildID := SetupGuild(t, k, ctx, founder, "Officer to Founder Guild", TestGuildDesc)
		SetupGuildFounderMembership(t, k, ctx, founder, guildID)
		AddMemberToGuild(t, k, ctx, newFounder, guildID)

		// Make new founder an officer
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.Officers = []string{newFounderStr}
		k.Guild.Set(ctx, guildID, guild)

		_, err := ms.TransferGuildFounder(ctx, &types.MsgTransferGuildFounder{
			Creator:    founderStr,
			NewFounder: newFounderStr,
			GuildId:    guildID,
		})

		require.NoError(t, err)

		// Verify new founder is no longer in officers list
		guild, _ = k.Guild.Get(ctx, guildID)
		require.Equal(t, newFounderStr, guild.Founder)
		require.NotContains(t, guild.Officers, newFounderStr)
	})
}
