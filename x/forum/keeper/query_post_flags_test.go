package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryPostFlags(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.PostFlags(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("zero post_id", func(t *testing.T) {
		_, err := qs.PostFlags(f.ctx, &types.QueryPostFlagsRequest{PostId: 0})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no flags", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		resp, err := qs.PostFlags(f.ctx, &types.QueryPostFlagsRequest{PostId: post.PostId})
		require.NoError(t, err)
		require.Equal(t, "0", resp.TotalWeight)
		require.False(t, resp.InReviewQueue)
		require.Equal(t, uint64(0), resp.FlaggerCount)
	})

	t.Run("has flags", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		// Create flag record
		flag := types.PostFlag{
			PostId:        post.PostId,
			TotalWeight:   "150",
			InReviewQueue: true,
			Flaggers:      []string{testCreator2, testSentinel},
		}
		f.keeper.PostFlag.Set(f.ctx, post.PostId, flag)

		resp, err := qs.PostFlags(f.ctx, &types.QueryPostFlagsRequest{PostId: post.PostId})
		require.NoError(t, err)
		require.Equal(t, "150", resp.TotalWeight)
		require.True(t, resp.InReviewQueue)
		require.Equal(t, uint64(2), resp.FlaggerCount)
	})
}
