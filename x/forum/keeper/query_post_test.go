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

func createNPost(keeper keeper.Keeper, ctx context.Context, n int) []types.Post {
	items := make([]types.Post, n)
	for i := range items {
		items[i].PostId = uint64(i)
		items[i].CategoryId = uint64(i)
		items[i].RootId = uint64(i)
		items[i].ParentId = uint64(i)
		items[i].Author = strconv.Itoa(i)
		items[i].Content = strconv.Itoa(i)
		items[i].CreatedAt = int64(i)
		items[i].ExpirationTime = int64(i)
		items[i].Status = uint64(i)
		items[i].HiddenBy = strconv.Itoa(i)
		items[i].HiddenAt = int64(i)
		items[i].Pinned = true
		items[i].PinnedBy = strconv.Itoa(i)
		items[i].PinnedAt = int64(i)
		items[i].PinPriority = uint64(i)
		items[i].Locked = true
		items[i].LockedBy = strconv.Itoa(i)
		items[i].LockedAt = int64(i)
		items[i].LockReason = strconv.Itoa(i)
		items[i].UpvoteCount = uint64(i)
		items[i].DownvoteCount = uint64(i)
		items[i].Depth = uint64(i)
		items[i].Edited = true
		items[i].EditedAt = int64(i)
		_ = keeper.Post.Set(ctx, items[i].PostId, items[i])
	}
	return items
}

func TestPostQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNPost(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetPostRequest
		response *types.QueryGetPostResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetPostRequest{
				PostId: msgs[0].PostId,
			},
			response: &types.QueryGetPostResponse{Post: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetPostRequest{
				PostId: msgs[1].PostId,
			},
			response: &types.QueryGetPostResponse{Post: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetPostRequest{
				PostId: 100000,
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
			response, err := qs.GetPost(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestPostQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNPost(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllPostRequest {
		return &types.QueryAllPostRequest{
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
			resp, err := qs.ListPost(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Post), step)
			require.Subset(t, msgs, resp.Post)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListPost(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Post), step)
			require.Subset(t, msgs, resp.Post)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListPost(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.Post)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListPost(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
