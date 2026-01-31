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

func TestQueryMemberGuildInvites(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.MemberGuildInvites(ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty member address", func(t *testing.T) {
		_, err := qs.MemberGuildInvites(ctx, &types.QueryMemberGuildInvitesRequest{Member: ""})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("member with no invites", func(t *testing.T) {
		member := TestAddrMember1
		memberStr, _ := f.addressCodec.BytesToString(member)

		resp, err := qs.MemberGuildInvites(ctx, &types.QueryMemberGuildInvitesRequest{Member: memberStr})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.GuildId)
	})

	t.Run("member with invite", func(t *testing.T) {
		founder := TestAddrFounder
		invitee := TestAddrMember2
		founderStr, _ := f.addressCodec.BytesToString(founder)
		inviteeStr, _ := f.addressCodec.BytesToString(invitee)

		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, "Invite Test Guild", TestGuildDesc)

		// Create invite
		key := fmt.Sprintf("%d:%s", guildID, inviteeStr)
		invite := types.GuildInvite{
			GuildInvitee: inviteeStr,
			Inviter:      founderStr,
		}
		k.GuildInvite.Set(ctx, key, invite)

		resp, err := qs.MemberGuildInvites(ctx, &types.QueryMemberGuildInvitesRequest{Member: inviteeStr})
		require.NoError(t, err)
		require.Equal(t, guildID, resp.GuildId)
		require.Equal(t, "Invite Test Guild", resp.GuildName)
	})
}
