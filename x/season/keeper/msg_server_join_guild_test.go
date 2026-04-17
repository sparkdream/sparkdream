package keeper_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerJoinGuild(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.JoinGuild(f.ctx, &types.MsgJoinGuild{
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

		SetupBasicMemberProfile(t, k, ctx, creator)

		_, err := ms.JoinGuild(ctx, &types.MsgJoinGuild{
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
		joiner := TestAddrMember1
		joinerStr, _ := f.addressCodec.BytesToString(joiner)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, joiner)
		guildID := SetupGuild(t, k, ctx, founder, "Dissolved Guild", TestGuildDesc)

		// Mark as dissolved
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.Status = types.GuildStatus_GUILD_STATUS_DISSOLVED
		k.Guild.Set(ctx, guildID, guild)

		_, err := ms.JoinGuild(ctx, &types.MsgJoinGuild{
			Creator: joinerStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildDissolved)
	})

	t.Run("profile not found", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		joiner := TestAddrMember1
		joinerStr, _ := f.addressCodec.BytesToString(joiner)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)
		// Don't setup joiner profile

		_, err := ms.JoinGuild(ctx, &types.MsgJoinGuild{
			Creator: joinerStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "profile not found")
	})

	t.Run("already in a guild", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		joiner := TestAddrMember1
		joinerStr, _ := f.addressCodec.BytesToString(joiner)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		// Joiner is already in another guild
		SetupMemberProfileWithGuild(t, k, ctx, joiner, 999)

		_, err := ms.JoinGuild(ctx, &types.MsgJoinGuild{
			Creator: joinerStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrAlreadyInGuild)
	})

	t.Run("invite only guild without invite", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		joiner := TestAddrMember1
		joinerStr, _ := f.addressCodec.BytesToString(joiner)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, joiner)
		guildID := SetupGuild(t, k, ctx, founder, "Invite Only Guild", TestGuildDesc)

		// Make invite only
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.InviteOnly = true
		k.Guild.Set(ctx, guildID, guild)

		_, err := ms.JoinGuild(ctx, &types.MsgJoinGuild{
			Creator: joinerStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildInviteOnly)
	})

	t.Run("successful join public guild", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		joiner := TestAddrMember2
		joinerStr, _ := f.addressCodec.BytesToString(joiner)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, joiner)
		guildID := SetupGuild(t, k, ctx, founder, "Public Guild", TestGuildDesc)

		_, err := ms.JoinGuild(ctx, &types.MsgJoinGuild{
			Creator: joinerStr,
			GuildId: guildID,
		})

		require.NoError(t, err)

		// Verify profile was updated
		profile, _ := k.MemberProfile.Get(ctx, joinerStr)
		require.Equal(t, guildID, profile.GuildId)

		// Verify membership was created
		membership, err := k.GuildMembership.Get(ctx, joinerStr)
		require.NoError(t, err)
		require.Equal(t, guildID, membership.GuildId)
		require.Equal(t, uint64(1), membership.GuildsJoinedThisSeason)

		// Guild member counter should have been incremented on join.
		require.Equal(t, uint64(1), k.GetGuildMemberCount(ctx, guildID))
	})

	t.Run("successful join invite only guild with invite", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		joiner := TestAddrMember3
		joinerStr, _ := f.addressCodec.BytesToString(joiner)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, joiner)
		guildID := SetupGuild(t, k, ctx, founder, "Private Guild", TestGuildDesc)

		// Make invite only and add invite
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.InviteOnly = true
		guild.PendingInvites = []string{joinerStr}
		k.Guild.Set(ctx, guildID, guild)

		// Create invite record
		key := fmt.Sprintf("%d:%s", guildID, joinerStr)
		invite := types.GuildInvite{
			GuildInvitee: joinerStr,
			Inviter:      founder.String(),
		}
		k.GuildInvite.Set(ctx, key, invite)

		_, err := ms.JoinGuild(ctx, &types.MsgJoinGuild{
			Creator: joinerStr,
			GuildId: guildID,
		})

		require.NoError(t, err)

		// Verify invite was removed
		guild, _ = k.Guild.Get(ctx, guildID)
		require.NotContains(t, guild.PendingInvites, joinerStr)
	})

	t.Run("guild hop cooldown enforced", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		joiner := TestAddrCreator
		joinerStr, _ := f.addressCodec.BytesToString(joiner)

		params, _ := k.Params.Get(ctx)
		// Advance to epoch 5
		ctx = ctx.WithBlockHeight(5 * params.EpochBlocks)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, joiner)
		guildID := SetupGuild(t, k, ctx, founder, "New Guild", TestGuildDesc)

		// Joiner recently left another guild
		membership := types.GuildMembership{
			Member:    joinerStr,
			GuildId:   0,
			LeftEpoch: 4, // Left at epoch 4
		}
		k.GuildMembership.Set(ctx, joinerStr, membership)

		_, err := ms.JoinGuild(ctx, &types.MsgJoinGuild{
			Creator: joinerStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildHopCooldown)
	})
}
