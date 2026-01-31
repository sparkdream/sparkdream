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

func TestQuerySeasonByNumber(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.SeasonByNumber(ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("query current season number", func(t *testing.T) {
		// Setup season
		season := types.Season{
			Number:     3,
			Name:       "Season Three",
			Theme:      "Adventure",
			StartBlock: 100,
			EndBlock:   10000,
			Status:     types.SeasonStatus_SEASON_STATUS_ACTIVE,
		}
		k.Season.Set(ctx, season)

		resp, err := qs.SeasonByNumber(ctx, &types.QuerySeasonByNumberRequest{Number: 3})
		require.NoError(t, err)
		require.Equal(t, "Season Three", resp.Name)
		require.Equal(t, int64(100), resp.StartBlock)
		require.Equal(t, int64(10000), resp.EndBlock)
	})

	t.Run("query historical season", func(t *testing.T) {
		// Store a snapshot for a historical season
		snapshot := types.SeasonSnapshot{
			Season:        2,
			SnapshotBlock: 5000,
		}
		k.SeasonSnapshot.Set(ctx, 2, snapshot)

		resp, err := qs.SeasonByNumber(ctx, &types.QuerySeasonByNumberRequest{Number: 2})
		require.NoError(t, err)
		require.Equal(t, int64(5000), resp.EndBlock)
		require.Equal(t, uint64(types.SeasonStatus_SEASON_STATUS_COMPLETED), resp.Status)
	})

	t.Run("season number not found", func(t *testing.T) {
		_, err := qs.SeasonByNumber(ctx, &types.QuerySeasonByNumberRequest{Number: 999})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, st.Code())
	})
}
