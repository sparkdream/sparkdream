package keeper_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestQueryGuildInvites(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.GuildInvites(ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("guild not found", func(t *testing.T) {
		_, err := qs.GuildInvites(ctx, &types.QueryGuildInvitesRequest{GuildId: 999})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("guild with no invites", func(t *testing.T) {
		founder := TestAddrFounder
		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, "No Invites Guild", TestGuildDesc)

		resp, err := qs.GuildInvites(ctx, &types.QueryGuildInvitesRequest{GuildId: guildID})
		require.NoError(t, err)
		require.Empty(t, resp.Invitee)
	})

	t.Run("guild with invites", func(t *testing.T) {
		founder := TestAddrMember1
		invitee := TestAddrMember2
		founderStr, _ := f.addressCodec.BytesToString(founder)
		inviteeStr, _ := f.addressCodec.BytesToString(invitee)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, "Invites Guild", TestGuildDesc)

		// Add invite
		key := fmt.Sprintf("%d:%s", guildID, inviteeStr)
		invite := types.GuildInvite{
			GuildInvitee: inviteeStr,
			Inviter:      founderStr,
		}
		k.GuildInvite.Set(ctx, key, invite)

		// Update guild's pending invites
		guild, _ := k.Guild.Get(ctx, guildID)
		guild.PendingInvites = []string{inviteeStr}
		k.Guild.Set(ctx, guildID, guild)

		resp, err := qs.GuildInvites(ctx, &types.QueryGuildInvitesRequest{GuildId: guildID})
		require.NoError(t, err)
		require.Equal(t, inviteeStr, resp.Invitee)
	})
}
