package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerSetGuildInviteOnly(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.SetGuildInviteOnly(f.ctx, &types.MsgSetGuildInviteOnly{
			Creator:    "invalid-address",
			GuildId:    1,
			InviteOnly: true,
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

		_, err := ms.SetGuildInviteOnly(ctx, &types.MsgSetGuildInviteOnly{
			Creator:    creatorStr,
			GuildId:    999,
			InviteOnly: true,
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
		founderStr, _ := f.addressCodec.BytesToString(founder)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		// Set guild to dissolved
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.Status = types.GuildStatus_GUILD_STATUS_DISSOLVED
		k.Guild.Set(ctx, guildID, guild)

		_, err := ms.SetGuildInviteOnly(ctx, &types.MsgSetGuildInviteOnly{
			Creator:    founderStr,
			GuildId:    guildID,
			InviteOnly: true,
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
		nonFounderStr, _ := f.addressCodec.BytesToString(nonFounder)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, nonFounder)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		_, err := ms.SetGuildInviteOnly(ctx, &types.MsgSetGuildInviteOnly{
			Creator:    nonFounderStr,
			GuildId:    guildID,
			InviteOnly: true,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotGuildFounder)
	})

	t.Run("successful enable invite only", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		founderStr, _ := f.addressCodec.BytesToString(founder)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, "Invite Only Guild", TestGuildDesc)

		// Verify starts as open
		guild, _ := k.Guild.Get(ctx, guildID)
		require.False(t, guild.InviteOnly)

		_, err := ms.SetGuildInviteOnly(ctx, &types.MsgSetGuildInviteOnly{
			Creator:    founderStr,
			GuildId:    guildID,
			InviteOnly: true,
		})

		require.NoError(t, err)

		// Verify invite only was enabled
		guild, _ = k.Guild.Get(ctx, guildID)
		require.True(t, guild.InviteOnly)
	})

	t.Run("successful disable invite only", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		founderStr, _ := f.addressCodec.BytesToString(founder)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, "Open Guild", TestGuildDesc)

		// Set to invite only first
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.InviteOnly = true
		k.Guild.Set(ctx, guildID, guild)

		_, err := ms.SetGuildInviteOnly(ctx, &types.MsgSetGuildInviteOnly{
			Creator:    founderStr,
			GuildId:    guildID,
			InviteOnly: false,
		})

		require.NoError(t, err)

		// Verify invite only was disabled
		guild, _ = k.Guild.Get(ctx, guildID)
		require.False(t, guild.InviteOnly)
	})

	t.Run("set to same value no error", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		founderStr, _ := f.addressCodec.BytesToString(founder)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, "Same Value Guild", TestGuildDesc)

		// Guild starts as open (InviteOnly = false)
		_, err := ms.SetGuildInviteOnly(ctx, &types.MsgSetGuildInviteOnly{
			Creator:    founderStr,
			GuildId:    guildID,
			InviteOnly: false, // Same as current
		})

		require.NoError(t, err)
	})
}
