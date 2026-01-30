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

func createNThreadFollowCount(keeper keeper.Keeper, ctx context.Context, n int) []types.ThreadFollowCount {
	items := make([]types.ThreadFollowCount, n)
	for i := range items {
		items[i].ThreadId = uint64(i)
		items[i].FollowerCount = uint64(i)
		_ = keeper.ThreadFollowCount.Set(ctx, items[i].ThreadId, items[i])
	}
	return items
}

func TestThreadFollowCountQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNThreadFollowCount(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetThreadFollowCountRequest
		response *types.QueryGetThreadFollowCountResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetThreadFollowCountRequest{
				ThreadId: msgs[0].ThreadId,
			},
			response: &types.QueryGetThreadFollowCountResponse{ThreadFollowCount: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetThreadFollowCountRequest{
				ThreadId: msgs[1].ThreadId,
			},
			response: &types.QueryGetThreadFollowCountResponse{ThreadFollowCount: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetThreadFollowCountRequest{
				ThreadId: 100000,
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
			response, err := qs.GetThreadFollowCount(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestThreadFollowCountQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNThreadFollowCount(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllThreadFollowCountRequest {
		return &types.QueryAllThreadFollowCountRequest{
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
			resp, err := qs.ListThreadFollowCount(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.ThreadFollowCount), step)
			require.Subset(t, msgs, resp.ThreadFollowCount)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListThreadFollowCount(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.ThreadFollowCount), step)
			require.Subset(t, msgs, resp.ThreadFollowCount)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListThreadFollowCount(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.ThreadFollowCount)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListThreadFollowCount(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
