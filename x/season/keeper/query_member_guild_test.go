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

func TestQueryMemberGuild(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.MemberGuild(ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty member address", func(t *testing.T) {
		_, err := qs.MemberGuild(ctx, &types.QueryMemberGuildRequest{Member: ""})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("member not in any guild", func(t *testing.T) {
		member := TestAddrMember1
		memberStr, _ := f.addressCodec.BytesToString(member)

		resp, err := qs.MemberGuild(ctx, &types.QueryMemberGuildRequest{Member: memberStr})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.GuildId)
		require.Equal(t, int64(0), resp.JoinedEpoch)
	})

	t.Run("member in guild", func(t *testing.T) {
		founder := TestAddrFounder
		member := TestAddrMember2
		memberStr, _ := f.addressCodec.BytesToString(member)

		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupBasicMemberProfile(t, k, ctx, member)
		guildID := SetupGuild(t, k, ctx, founder, "Member Guild Test", TestGuildDesc)
		AddMemberToGuild(t, k, ctx, member, guildID)

		resp, err := qs.MemberGuild(ctx, &types.QueryMemberGuildRequest{Member: memberStr})
		require.NoError(t, err)
		require.Equal(t, guildID, resp.GuildId)
	})

	t.Run("member left guild", func(t *testing.T) {
		member := TestAddrMember3
		memberStr, _ := f.addressCodec.BytesToString(member)

		// Create membership with LeftEpoch set (left guild)
		membership := types.GuildMembership{
			Member:      memberStr,
			GuildId:     1,
			JoinedEpoch: 5,
			LeftEpoch:   10, // Left at epoch 10
		}
		k.GuildMembership.Set(ctx, memberStr, membership)

		resp, err := qs.MemberGuild(ctx, &types.QueryMemberGuildRequest{Member: memberStr})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.GuildId) // Returns 0 since member left
	})
}
