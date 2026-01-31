package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryFlagReviewQueue(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.FlagReviewQueue(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty queue", func(t *testing.T) {
		resp, err := qs.FlagReviewQueue(f.ctx, &types.QueryFlagReviewQueueRequest{})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.PostId)
	})

	t.Run("has flagged post in queue", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		// Create a post flag in review queue
		flag := types.PostFlag{
			PostId:        post.PostId,
			TotalWeight:   "100",
			InReviewQueue: true,
		}
		f.keeper.PostFlag.Set(f.ctx, post.PostId, flag)

		resp, err := qs.FlagReviewQueue(f.ctx, &types.QueryFlagReviewQueueRequest{})
		require.NoError(t, err)
		require.Equal(t, post.PostId, resp.PostId)
	})

	t.Run("ignores flags not in review queue", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		// Create a post flag NOT in review queue
		flag := types.PostFlag{
			PostId:        post.PostId,
			TotalWeight:   "50",
			InReviewQueue: false,
		}
		f.keeper.PostFlag.Set(f.ctx, post.PostId, flag)

		// Clear any previous flags
		resp, err := qs.FlagReviewQueue(f.ctx, &types.QueryFlagReviewQueueRequest{})
		require.NoError(t, err)
		// Should not return this flag since it's not in review queue
		require.NotEqual(t, post.PostId, resp.PostId)
	})
}
