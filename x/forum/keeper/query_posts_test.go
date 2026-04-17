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
		require.Empty(t, resp.Posts)
	})

	t.Run("returns root posts", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		resp, err := qs.Posts(f.ctx, &types.QueryPostsRequest{})
		require.NoError(t, err)
		require.NotEmpty(t, resp.Posts)
		found := false
		for _, p := range resp.Posts {
			if p.PostId == post.PostId {
				found = true
				require.Equal(t, testCreator, p.Author)
			}
		}
		require.True(t, found, "created post not returned")
	})

	t.Run("filter by category", func(t *testing.T) {
		cat := f.createTestCategory(t, "Test Category")
		post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

		resp, err := qs.Posts(f.ctx, &types.QueryPostsRequest{CategoryId: cat.CategoryId})
		require.NoError(t, err)
		require.Len(t, resp.Posts, 1)
		require.Equal(t, post.PostId, resp.Posts[0].PostId)
	})

	t.Run("filter by status", func(t *testing.T) {
		f.createTestPost(t, testCreator, 0, 0)

		resp, err := qs.Posts(f.ctx, &types.QueryPostsRequest{Status: uint64(types.PostStatus_POST_STATUS_ACTIVE)})
		require.NoError(t, err)
		require.NotEmpty(t, resp.Posts)
		for _, p := range resp.Posts {
			require.Equal(t, types.PostStatus_POST_STATUS_ACTIVE, p.Status)
		}
	})

	t.Run("excludes replies", func(t *testing.T) {
		rootPost := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator, rootPost.PostId, 0)

		resp, err := qs.Posts(f.ctx, &types.QueryPostsRequest{})
		require.NoError(t, err)
		for _, p := range resp.Posts {
			require.NotEqual(t, reply.PostId, p.PostId)
			require.Equal(t, uint64(0), p.ParentId)
		}
	})
}
