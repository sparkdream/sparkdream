package keeper_test

import (
	"context"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func createNArchivedThread(keeper keeper.Keeper, ctx context.Context, n int) []types.ArchivedThread {
	items := make([]types.ArchivedThread, n)
	for i := range items {
		items[i].RootId = uint64(i)
		items[i].ArchivedAt = int64(i)
		items[i].PostCount = uint64(i)
		items[i].LastUnarchivedAt = int64(i)
		_ = keeper.ArchivedThread.Set(ctx, items[i].RootId, items[i])
	}
	return items
}

func TestArchivedThreadQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNArchivedThread(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetArchivedThreadRequest
		response *types.QueryGetArchivedThreadResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetArchivedThreadRequest{
				RootId: msgs[0].RootId,
			},
			response: &types.QueryGetArchivedThreadResponse{ArchivedThread: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetArchivedThreadRequest{
				RootId: msgs[1].RootId,
			},
			response: &types.QueryGetArchivedThreadResponse{ArchivedThread: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetArchivedThreadRequest{
				RootId: 100000,
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
			response, err := qs.GetArchivedThread(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestArchivedThreadQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNArchivedThread(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllArchivedThreadRequest {
		return &types.QueryAllArchivedThreadRequest{
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
			resp, err := qs.ListArchivedThread(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.ArchivedThread), step)
			require.Subset(t, msgs, resp.ArchivedThread)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListArchivedThread(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.ArchivedThread), step)
			require.Subset(t, msgs, resp.ArchivedThread)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListArchivedThread(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.ArchivedThread)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListArchivedThread(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
