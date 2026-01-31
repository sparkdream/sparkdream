package keeper_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerRevokeGuildInvite(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.RevokeGuildInvite(f.ctx, &types.MsgRevokeGuildInvite{
			Creator: "invalid-address",
			Invitee: TestAddrMember1.String(),
			GuildId: 1,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("invalid invitee address", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		_, err := ms.RevokeGuildInvite(ctx, &types.MsgRevokeGuildInvite{
			Creator: creatorStr,
			Invitee: "invalid-address",
			GuildId: 1,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid invitee address")
	})

	t.Run("no invite exists", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		invitee := TestAddrMember1
		founderStr, _ := f.addressCodec.BytesToString(founder)
		inviteeStr, _ := f.addressCodec.BytesToString(invitee)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		_, err := ms.RevokeGuildInvite(ctx, &types.MsgRevokeGuildInvite{
			Creator: founderStr,
			Invitee: inviteeStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNoGuildInvite)
	})

	t.Run("not founder or officer or invitee", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		invitee := TestAddrMember1
		randomUser := TestAddrMember2
		inviteeStr, _ := f.addressCodec.BytesToString(invitee)
		randomStr, _ := f.addressCodec.BytesToString(randomUser)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		// Create invite
		key := fmt.Sprintf("%d:%s", guildID, inviteeStr)
		invite := types.GuildInvite{GuildInvitee: inviteeStr}
		k.GuildInvite.Set(ctx, key, invite)

		_, err := ms.RevokeGuildInvite(ctx, &types.MsgRevokeGuildInvite{
			Creator: randomStr, // Not founder, not invitee
			Invitee: inviteeStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotGuildFounderOrOfficer)
	})

	t.Run("successful revoke by founder", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		invitee := TestAddrMember1
		founderStr, _ := f.addressCodec.BytesToString(founder)
		inviteeStr, _ := f.addressCodec.BytesToString(invitee)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, "Revoke Test Guild", TestGuildDesc)

		// Add invite
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.PendingInvites = []string{inviteeStr}
		k.Guild.Set(ctx, guildID, guild)

		key := fmt.Sprintf("%d:%s", guildID, inviteeStr)
		invite := types.GuildInvite{
			GuildInvitee: inviteeStr,
			Inviter:      founderStr,
		}
		k.GuildInvite.Set(ctx, key, invite)

		_, err := ms.RevokeGuildInvite(ctx, &types.MsgRevokeGuildInvite{
			Creator: founderStr,
			Invitee: inviteeStr,
			GuildId: guildID,
		})

		require.NoError(t, err)

		// Verify invite was removed
		_, err = k.GuildInvite.Get(ctx, key)
		require.Error(t, err)

		// Verify removed from pending list
		guild, _ = k.Guild.Get(ctx, guildID)
		require.NotContains(t, guild.PendingInvites, inviteeStr)
	})

	t.Run("successful decline by invitee", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		invitee := TestAddrMember2
		founderStr, _ := f.addressCodec.BytesToString(founder)
		inviteeStr, _ := f.addressCodec.BytesToString(invitee)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, "Decline Test Guild", TestGuildDesc)

		// Add invite
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.PendingInvites = []string{inviteeStr}
		k.Guild.Set(ctx, guildID, guild)

		key := fmt.Sprintf("%d:%s", guildID, inviteeStr)
		invite := types.GuildInvite{
			GuildInvitee: inviteeStr,
			Inviter:      founderStr,
		}
		k.GuildInvite.Set(ctx, key, invite)

		// Invitee declines (revokes their own invite)
		_, err := ms.RevokeGuildInvite(ctx, &types.MsgRevokeGuildInvite{
			Creator: inviteeStr, // Invitee is revoking
			Invitee: inviteeStr,
			GuildId: guildID,
		})

		require.NoError(t, err)

		// Verify invite was removed
		_, err = k.GuildInvite.Get(ctx, key)
		require.Error(t, err)
	})

	t.Run("successful revoke by officer", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		officer := TestAddrOfficer
		invitee := TestAddrMember3
		founderStr, _ := f.addressCodec.BytesToString(founder)
		officerStr, _ := f.addressCodec.BytesToString(officer)
		inviteeStr, _ := f.addressCodec.BytesToString(invitee)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, officer)
		guildID := SetupGuild(t, k, ctx, founder, "Officer Revoke Guild", TestGuildDesc)

		// Make officer and add invite
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.Officers = []string{officerStr}
		guild.PendingInvites = []string{inviteeStr}
		k.Guild.Set(ctx, guildID, guild)

		key := fmt.Sprintf("%d:%s", guildID, inviteeStr)
		invite := types.GuildInvite{
			GuildInvitee: inviteeStr,
			Inviter:      founderStr,
		}
		k.GuildInvite.Set(ctx, key, invite)

		_, err := ms.RevokeGuildInvite(ctx, &types.MsgRevokeGuildInvite{
			Creator: officerStr,
			Invitee: inviteeStr,
			GuildId: guildID,
		})

		require.NoError(t, err)
	})
}
