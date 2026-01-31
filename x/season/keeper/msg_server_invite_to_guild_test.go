package keeper_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerInviteToGuild(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.InviteToGuild(f.ctx, &types.MsgInviteToGuild{
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

		_, err := ms.InviteToGuild(ctx, &types.MsgInviteToGuild{
			Creator: creatorStr,
			Invitee: "invalid-address",
			GuildId: 1,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid invitee address")
	})

	t.Run("guild not found", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		invitee := TestAddrMember1
		creatorStr, _ := f.addressCodec.BytesToString(creator)
		inviteeStr, _ := f.addressCodec.BytesToString(invitee)

		_, err := ms.InviteToGuild(ctx, &types.MsgInviteToGuild{
			Creator: creatorStr,
			Invitee: inviteeStr,
			GuildId: 999,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildNotFound)
	})

	t.Run("guild not active", func(t *testing.T) {
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

		// Mark as dissolved
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.Status = types.GuildStatus_GUILD_STATUS_DISSOLVED
		k.Guild.Set(ctx, guildID, guild)

		_, err := ms.InviteToGuild(ctx, &types.MsgInviteToGuild{
			Creator: founderStr,
			Invitee: inviteeStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrGuildDissolved)
	})

	t.Run("not founder or officer", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		nonOfficer := TestAddrMember1
		invitee := TestAddrMember2
		nonOfficerStr, _ := f.addressCodec.BytesToString(nonOfficer)
		inviteeStr, _ := f.addressCodec.BytesToString(invitee)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		_, err := ms.InviteToGuild(ctx, &types.MsgInviteToGuild{
			Creator: nonOfficerStr,
			Invitee: inviteeStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotGuildFounderOrOfficer)
	})

	t.Run("invitee has no profile", func(t *testing.T) {
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

		_, err := ms.InviteToGuild(ctx, &types.MsgInviteToGuild{
			Creator: founderStr,
			Invitee: inviteeStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invitee profile not found")
	})

	t.Run("invitee already in a guild", func(t *testing.T) {
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

		// Invitee is already in another guild
		SetupMemberProfileWithGuild(t, k, ctx, invitee, 999)

		_, err := ms.InviteToGuild(ctx, &types.MsgInviteToGuild{
			Creator: founderStr,
			Invitee: inviteeStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "already in a guild")
	})

	t.Run("already invited", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		invitee := TestAddrMember1
		founderStr, _ := f.addressCodec.BytesToString(founder)
		inviteeStr, _ := f.addressCodec.BytesToString(invitee)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, invitee)
		guildID := SetupGuild(t, k, ctx, founder, TestGuildName, TestGuildDesc)

		// Add existing invite
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.PendingInvites = []string{inviteeStr}
		k.Guild.Set(ctx, guildID, guild)

		key := fmt.Sprintf("%d:%s", guildID, inviteeStr)
		invite := types.GuildInvite{GuildInvitee: inviteeStr}
		k.GuildInvite.Set(ctx, key, invite)

		_, err := ms.InviteToGuild(ctx, &types.MsgInviteToGuild{
			Creator: founderStr,
			Invitee: inviteeStr,
			GuildId: guildID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrAlreadyInvited)
	})

	t.Run("successful invite by founder", func(t *testing.T) {
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
		guildID := SetupGuild(t, k, ctx, founder, "Invite Guild", TestGuildDesc)

		_, err := ms.InviteToGuild(ctx, &types.MsgInviteToGuild{
			Creator: founderStr,
			Invitee: inviteeStr,
			GuildId: guildID,
		})

		require.NoError(t, err)

		// Verify invite was created
		key := fmt.Sprintf("%d:%s", guildID, inviteeStr)
		invite, err := k.GuildInvite.Get(ctx, key)
		require.NoError(t, err)
		require.Equal(t, inviteeStr, invite.GuildInvitee)
		require.Equal(t, founderStr, invite.Inviter)

		// Verify pending invites list was updated
		guild, _ := k.Guild.Get(ctx, guildID)
		require.Contains(t, guild.PendingInvites, inviteeStr)
	})

	t.Run("successful invite by officer", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		founder := TestAddrFounder
		officer := TestAddrOfficer
		invitee := TestAddrMember3
		officerStr, _ := f.addressCodec.BytesToString(officer)
		inviteeStr, _ := f.addressCodec.BytesToString(invitee)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, officer)
		SetupBasicMemberProfile(t, k, ctx, invitee)
		guildID := SetupGuild(t, k, ctx, founder, "Officer Guild", TestGuildDesc)

		// Make officer
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.Officers = []string{officerStr}
		k.Guild.Set(ctx, guildID, guild)

		_, err := ms.InviteToGuild(ctx, &types.MsgInviteToGuild{
			Creator: officerStr,
			Invitee: inviteeStr,
			GuildId: guildID,
		})

		require.NoError(t, err)

		// Verify invite was created
		key := fmt.Sprintf("%d:%s", guildID, inviteeStr)
		invite, err := k.GuildInvite.Get(ctx, key)
		require.NoError(t, err)
		require.Equal(t, officerStr, invite.Inviter)
	})
}
