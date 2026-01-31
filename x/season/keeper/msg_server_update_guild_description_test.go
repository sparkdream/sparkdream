package keeper_test

import (
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerUpdateGuildDescription(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.UpdateGuildDescription(f.ctx, &types.MsgUpdateGuildDescription{
			Creator:     "invalid-address",
			GuildId:     1,
			Description: "New description",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("description too long", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		founderStr, _ := f.addressCodec.BytesToString(founder)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		// Create a very long description
		longDesc := strings.Repeat("a", 1001)

		_, err := ms.UpdateGuildDescription(ctx, &types.MsgUpdateGuildDescription{
			Creator:     founderStr,
			GuildId:     guildID,
			Description: longDesc,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildDescriptionTooLong)
	})

	t.Run("guild not found", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		_, err := ms.UpdateGuildDescription(ctx, &types.MsgUpdateGuildDescription{
			Creator:     creatorStr,
			GuildId:     999,
			Description: "New description",
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

		_, err := ms.UpdateGuildDescription(ctx, &types.MsgUpdateGuildDescription{
			Creator:     founderStr,
			GuildId:     guildID,
			Description: "New description",
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

		_, err := ms.UpdateGuildDescription(ctx, &types.MsgUpdateGuildDescription{
			Creator:     nonFounderStr,
			GuildId:     guildID,
			Description: "New description",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotGuildFounder)
	})

	t.Run("successful update", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		founderStr, _ := f.addressCodec.BytesToString(founder)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		newDesc := "This is the new guild description"
		_, err := ms.UpdateGuildDescription(ctx, &types.MsgUpdateGuildDescription{
			Creator:     founderStr,
			GuildId:     guildID,
			Description: newDesc,
		})

		require.NoError(t, err)

		// Verify description was updated
		guild, _ := k.Guild.Get(ctx, guildID)
		require.Equal(t, newDesc, guild.Description)
	})

	t.Run("update to empty description", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		founderStr, _ := f.addressCodec.BytesToString(founder)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, "Empty Desc Guild", "Original description")

		_, err := ms.UpdateGuildDescription(ctx, &types.MsgUpdateGuildDescription{
			Creator:     founderStr,
			GuildId:     guildID,
			Description: "",
		})

		require.NoError(t, err)

		// Verify description was cleared
		guild, _ := k.Guild.Get(ctx, guildID)
		require.Equal(t, "", guild.Description)
	})
}
