package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestQueryListRetroRewardHistory(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.ListRetroRewardHistory(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("returns records for matching season", func(t *testing.T) {
		// Create records for season 1
		rec1 := types.RetroRewardRecord{
			Season:             1,
			NominationId:       10,
			Recipient:          "cosmos1alice",
			ContentRef:         "blog/post/1",
			Conviction:         math.LegacyNewDec(500),
			RewardAmount:       math.LegacyNewDec(1000),
			DistributedAtBlock: 9000,
		}
		rec2 := types.RetroRewardRecord{
			Season:             1,
			NominationId:       11,
			Recipient:          "cosmos1bob",
			ContentRef:         "forum/post/5",
			Conviction:         math.LegacyNewDec(300),
			RewardAmount:       math.LegacyNewDec(600),
			DistributedAtBlock: 9000,
		}
		// Create a record for season 2 (should NOT be returned for season 1 query)
		rec3 := types.RetroRewardRecord{
			Season:             2,
			NominationId:       20,
			Recipient:          "cosmos1carol",
			ContentRef:         "collect/collection/3",
			Conviction:         math.LegacyNewDec(800),
			RewardAmount:       math.LegacyNewDec(1500),
			DistributedAtBlock: 18000,
		}

		err := f.keeper.RetroRewardRecord.Set(f.ctx, fmt.Sprintf("%d/%d", rec1.Season, rec1.NominationId), rec1)
		require.NoError(t, err)
		err = f.keeper.RetroRewardRecord.Set(f.ctx, fmt.Sprintf("%d/%d", rec2.Season, rec2.NominationId), rec2)
		require.NoError(t, err)
		err = f.keeper.RetroRewardRecord.Set(f.ctx, fmt.Sprintf("%d/%d", rec3.Season, rec3.NominationId), rec3)
		require.NoError(t, err)

		// Query season 1 records
		resp, err := qs.ListRetroRewardHistory(f.ctx, &types.QueryListRetroRewardHistoryRequest{
			Season: 1,
		})
		require.NoError(t, err)
		require.Len(t, resp.Records, 2)
		for _, rec := range resp.Records {
			require.Equal(t, uint64(1), rec.Season)
		}

		// Query season 2 records
		resp, err = qs.ListRetroRewardHistory(f.ctx, &types.QueryListRetroRewardHistoryRequest{
			Season: 2,
		})
		require.NoError(t, err)
		require.Len(t, resp.Records, 1)
		require.Equal(t, "cosmos1carol", resp.Records[0].Recipient)
	})

	t.Run("returns empty for non-matching season", func(t *testing.T) {
		resp, err := qs.ListRetroRewardHistory(f.ctx, &types.QueryListRetroRewardHistoryRequest{
			Season: 999,
		})
		require.NoError(t, err)
		require.Len(t, resp.Records, 0)
	})
}
