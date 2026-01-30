package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func createNThreadFollow(keeper keeper.Keeper, ctx context.Context, n int) []types.ThreadFollow {
	items := make([]types.ThreadFollow, n)
	for i := range items {
		items[i].Follower = strconv.Itoa(i)
		items[i].ThreadId = uint64(i)
		items[i].FollowedAt = int64(i)
		_ = keeper.ThreadFollow.Set(ctx, items[i].Follower, items[i])
	}
	return items
}

func TestThreadFollowQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNThreadFollow(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetThreadFollowRequest
		response *types.QueryGetThreadFollowResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetThreadFollowRequest{
				Follower: msgs[0].Follower,
			},
			response: &types.QueryGetThreadFollowResponse{ThreadFollow: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetThreadFollowRequest{
				Follower: msgs[1].Follower,
			},
			response: &types.QueryGetThreadFollowResponse{ThreadFollow: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetThreadFollowRequest{
				Follower: strconv.Itoa(100000),
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
			response, err := qs.GetThreadFollow(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestThreadFollowQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNThreadFollow(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllThreadFollowRequest {
		return &types.QueryAllThreadFollowRequest{
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
			resp, err := qs.ListThreadFollow(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.ThreadFollow), step)
			require.Subset(t, msgs, resp.ThreadFollow)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListThreadFollow(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.ThreadFollow), step)
			require.Subset(t, msgs, resp.ThreadFollow)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListThreadFollow(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.ThreadFollow)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListThreadFollow(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
