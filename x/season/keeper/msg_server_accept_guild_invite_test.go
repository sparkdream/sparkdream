package keeper_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerAcceptGuildInvite(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.AcceptGuildInvite(f.ctx, &types.MsgAcceptGuildInvite{
			Creator: "invalid-address",
			GuildId: 1,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("no invite exists", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		SetupBasicMemberProfile(t, k, ctx, creator)

		_, err := ms.AcceptGuildInvite(ctx, &types.MsgAcceptGuildInvite{
			Creator: creatorStr,
			GuildId: 1,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNoGuildInvite)
	})

	t.Run("invite expired", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		invitee := TestAddrMember1
		inviteeStr, _ := f.addressCodec.BytesToString(invitee)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, invitee)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		params, _ := k.Params.Get(ctx)
		// Advance time
		ctx = ctx.WithBlockHeight(10 * params.EpochBlocks)

		// Create expired invite
		key := fmt.Sprintf("%d:%s", guildID, inviteeStr)
		invite := types.GuildInvite{
			GuildInvitee: inviteeStr,
			Inviter:      founder.String(),
			ExpiresEpoch: 5, // Expired at epoch 5
		}
		k.GuildInvite.Set(ctx, key, invite)

		_, err := ms.AcceptGuildInvite(ctx, &types.MsgAcceptGuildInvite{
			Creator: inviteeStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildInviteExpired)
	})

	t.Run("guild not found", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		invitee := TestAddrMember1
		inviteeStr, _ := f.addressCodec.BytesToString(invitee)

		SetupBasicMemberProfile(t, k, ctx, invitee)

		// Create invite for non-existent guild
		key := fmt.Sprintf("%d:%s", 999, inviteeStr)
		invite := types.GuildInvite{
			GuildInvitee: inviteeStr,
			ExpiresEpoch: 0, // Never expires
		}
		k.GuildInvite.Set(ctx, key, invite)

		_, err := ms.AcceptGuildInvite(ctx, &types.MsgAcceptGuildInvite{
			Creator: inviteeStr,
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
		invitee := TestAddrMember1
		inviteeStr, _ := f.addressCodec.BytesToString(invitee)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, invitee)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		// Mark as dissolved
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.Status = types.GuildStatus_GUILD_STATUS_DISSOLVED
		k.Guild.Set(ctx, guildID, guild)

		// Create invite
		key := fmt.Sprintf("%d:%s", guildID, inviteeStr)
		invite := types.GuildInvite{GuildInvitee: inviteeStr}
		k.GuildInvite.Set(ctx, key, invite)

		_, err := ms.AcceptGuildInvite(ctx, &types.MsgAcceptGuildInvite{
			Creator: inviteeStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildDissolved)
	})

	t.Run("already in a guild", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		invitee := TestAddrMember1
		inviteeStr, _ := f.addressCodec.BytesToString(invitee)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		// Invitee is already in another guild
		SetupMemberProfileWithGuild(t, k, ctx, invitee, 999)

		// Create invite
		key := fmt.Sprintf("%d:%s", guildID, inviteeStr)
		invite := types.GuildInvite{GuildInvitee: inviteeStr}
		k.GuildInvite.Set(ctx, key, invite)

		_, err := ms.AcceptGuildInvite(ctx, &types.MsgAcceptGuildInvite{
			Creator: inviteeStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrAlreadyInGuild)
	})

	t.Run("successful accept", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		invitee := TestAddrMember2
		founderStr, _ := f.addressCodec.BytesToString(founder)
		inviteeStr, _ := f.addressCodec.BytesToString(invitee)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, invitee)
		guildID := SetupGuild(t, k, ctx, founder, "Accept Test Guild", TestGuildDesc)

		// Add pending invite to guild
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.PendingInvites = []string{inviteeStr}
		k.Guild.Set(ctx, guildID, guild)

		// Create invite
		key := fmt.Sprintf("%d:%s", guildID, inviteeStr)
		invite := types.GuildInvite{
			GuildInvitee: inviteeStr,
			Inviter:      founderStr,
		}
		k.GuildInvite.Set(ctx, key, invite)

		_, err := ms.AcceptGuildInvite(ctx, &types.MsgAcceptGuildInvite{
			Creator: inviteeStr,
			GuildId: guildID,
		})

		require.NoError(t, err)

		// Verify profile was updated
		profile, _ := k.MemberProfile.Get(ctx, inviteeStr)
		require.Equal(t, guildID, profile.GuildId)

		// Verify membership was created
		membership, err := k.GuildMembership.Get(ctx, inviteeStr)
		require.NoError(t, err)
		require.Equal(t, guildID, membership.GuildId)

		// Verify invite was removed from pending
		guild, _ = k.Guild.Get(ctx, guildID)
		require.NotContains(t, guild.PendingInvites, inviteeStr)

		// Verify invite record was removed
		_, err = k.GuildInvite.Get(ctx, key)
		require.Error(t, err)
	})

	t.Run("guild hop cooldown enforced", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		invitee := TestAddrMember3
		inviteeStr, _ := f.addressCodec.BytesToString(invitee)

		params, _ := k.Params.Get(ctx)
		// Advance to epoch 5
		ctx = ctx.WithBlockHeight(5 * params.EpochBlocks)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, invitee)
		guildID := SetupGuild(t, k, ctx, founder, "Cooldown Guild", TestGuildDesc)

		// Invitee recently left another guild
		membership := types.GuildMembership{
			Member:    inviteeStr,
			GuildId:   0,
			LeftEpoch: 4, // Left at epoch 4
		}
		k.GuildMembership.Set(ctx, inviteeStr, membership)

		// Create invite
		key := fmt.Sprintf("%d:%s", guildID, inviteeStr)
		invite := types.GuildInvite{GuildInvitee: inviteeStr}
		k.GuildInvite.Set(ctx, key, invite)

		_, err := ms.AcceptGuildInvite(ctx, &types.MsgAcceptGuildInvite{
			Creator: inviteeStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildHopCooldown)
	})
}
