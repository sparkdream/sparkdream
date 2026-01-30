package keeper_test

import (
	"context"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func createNSeasonSnapshot(keeper keeper.Keeper, ctx context.Context, n int) []types.SeasonSnapshot {
	items := make([]types.SeasonSnapshot, n)
	for i := range items {
		items[i].Season = uint64(i)
		items[i].SnapshotBlock = int64(i)
		_ = keeper.SeasonSnapshot.Set(ctx, items[i].Season, items[i])
	}
	return items
}

func TestSeasonSnapshotQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNSeasonSnapshot(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetSeasonSnapshotRequest
		response *types.QueryGetSeasonSnapshotResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetSeasonSnapshotRequest{
				Season: msgs[0].Season,
			},
			response: &types.QueryGetSeasonSnapshotResponse{SeasonSnapshot: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetSeasonSnapshotRequest{
				Season: msgs[1].Season,
			},
			response: &types.QueryGetSeasonSnapshotResponse{SeasonSnapshot: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetSeasonSnapshotRequest{
				Season: 100000,
			},
			err: status.Error(codes.NotFound, "not found"),
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.GetSeasonSnapshot(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestSeasonSnapshotQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNSeasonSnapshot(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllSeasonSnapshotRequest {
		return &types.QueryAllSeasonSnapshotRequest{
			Pagination: &query.PageRequest{
				Key:        next,
				Offset:     offset,
				Limit:      limit,
				CountTotal: total,
			},
		}
	}
	t.Run("ByOffset", func(t *testing.T) {
		step := 2
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListSeasonSnapshot(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.SeasonSnapshot), step)
			require.Subset(t, msgs, resp.SeasonSnapshot)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListSeasonSnapshot(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.SeasonSnapshot), step)
			require.Subset(t, msgs, resp.SeasonSnapshot)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListSeasonSnapshot(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.SeasonSnapshot)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListSeasonSnapshot(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
