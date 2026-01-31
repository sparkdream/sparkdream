package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestQueryCurrentSeason(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.CurrentSeason(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("season not found", func(t *testing.T) {
		_, err := qs.CurrentSeason(f.ctx, &types.QueryCurrentSeasonRequest{})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("successful query", func(t *testing.T) {
		// Setup a season
		season := types.Season{
			Number:     1,
			Name:       "Test Season",
			Theme:      "Testing Theme",
			StartBlock: 100,
			EndBlock:   10000,
			Status:     types.SeasonStatus_SEASON_STATUS_ACTIVE,
		}
		err := f.keeper.Season.Set(f.ctx, season)
		require.NoError(t, err)

		resp, err := qs.CurrentSeason(f.ctx, &types.QueryCurrentSeasonRequest{})
		require.NoError(t, err)
		require.Equal(t, uint64(1), resp.Number)
		require.Equal(t, "Test Season", resp.Name)
		require.Equal(t, "Testing Theme", resp.Theme)
		require.Equal(t, int64(100), resp.StartBlock)
		require.Equal(t, int64(10000), resp.EndBlock)
		require.Equal(t, uint64(types.SeasonStatus_SEASON_STATUS_ACTIVE), resp.Status)
	})
}
