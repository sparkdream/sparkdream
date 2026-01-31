package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestQueryGuildMembers(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.GuildMembers(ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("guild not found", func(t *testing.T) {
		_, err := qs.GuildMembers(ctx, &types.QueryGuildMembersRequest{GuildId: 999})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("guild with no members", func(t *testing.T) {
		founder := TestAddrFounder
		SetupBasicMemberProfile(t, k, ctx, founder)
		guildID := SetupGuild(t, k, ctx, founder, "Empty Members Guild", TestGuildDesc)

		resp, err := qs.GuildMembers(ctx, &types.QueryGuildMembersRequest{GuildId: guildID})
		require.NoError(t, err)
		require.Empty(t, resp.Member)
	})

	t.Run("guild with members", func(t *testing.T) {
		founder := TestAddrMember1
		member := TestAddrMember2
		memberStr, _ := f.addressCodec.BytesToString(member)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, member)
		guildID := SetupGuild(t, k, ctx, founder, "Members Guild", TestGuildDesc)
		SetupGuildFounderMembership(t, k, ctx, founder, guildID)
		AddMemberToGuild(t, k, ctx, member, guildID)

		resp, err := qs.GuildMembers(ctx, &types.QueryGuildMembersRequest{GuildId: guildID})
		require.NoError(t, err)
		// Should have at least one member (could be founder or member)
		require.NotEmpty(t, resp.Member)
		// Check that our added member is found
		found := resp.Member == memberStr
		if !found {
			founderStr, _ := f.addressCodec.BytesToString(founder)
			found = resp.Member == founderStr
		}
		require.True(t, found)
	})
}
