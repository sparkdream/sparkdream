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

func TestQuerySeasonStats(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.SeasonStats(ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no season exists", func(t *testing.T) {
		_, err := qs.SeasonStats(ctx, &types.QuerySeasonStatsRequest{})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("successful query with stats", func(t *testing.T) {
		SetupDefaultSeason(t, k, ctx)

		// Add some members and guilds for stats
		member1 := TestAddrMember1
		member2 := TestAddrMember2
		founder := TestAddrFounder

		SetupBasicMemberProfile(t, k, ctx, member1)
		SetupBasicMemberProfile(t, k, ctx, member2)
		SetupBasicMemberProfile(t, k, ctx, founder)
		SetupGuild(t, k, ctx, founder, "Stats Test Guild", TestGuildDesc)

		resp, err := qs.SeasonStats(ctx, &types.QuerySeasonStatsRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		// Should have some stats (exact values depend on implementation)
	})
}
