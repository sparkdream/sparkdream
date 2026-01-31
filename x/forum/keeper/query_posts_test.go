package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryPosts(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.Posts(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no posts", func(t *testing.T) {
		resp, err := qs.Posts(f.ctx, &types.QueryPostsRequest{})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.PostId)
	})

	t.Run("returns root posts", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		resp, err := qs.Posts(f.ctx, &types.QueryPostsRequest{})
		require.NoError(t, err)
		require.Equal(t, post.PostId, resp.PostId)
		require.Equal(t, testCreator, resp.Author)
	})

	t.Run("filter by category", func(t *testing.T) {
		cat := f.createTestCategory(t, "Test Category")
		post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

		resp, err := qs.Posts(f.ctx, &types.QueryPostsRequest{CategoryId: cat.CategoryId})
		require.NoError(t, err)
		require.Equal(t, post.PostId, resp.PostId)
	})

	t.Run("filter by status", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		resp, err := qs.Posts(f.ctx, &types.QueryPostsRequest{Status: uint64(types.PostStatus_POST_STATUS_ACTIVE)})
		require.NoError(t, err)
		// Should return an active post (may not be the one we just created due to existing data)
		require.NotZero(t, resp.PostId)
		require.Equal(t, uint64(types.PostStatus_POST_STATUS_ACTIVE), resp.Status)
		_ = post // Use post to avoid compiler warning
	})

	t.Run("excludes replies", func(t *testing.T) {
		rootPost := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator, rootPost.PostId, 0)

		resp, err := qs.Posts(f.ctx, &types.QueryPostsRequest{})
		require.NoError(t, err)
		// Should return a root post (not the reply)
		require.NotEqual(t, reply.PostId, resp.PostId)
	})
}
