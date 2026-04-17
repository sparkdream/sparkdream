package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryUserPosts(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.UserPosts(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty author", func(t *testing.T) {
		_, err := qs.UserPosts(f.ctx, &types.QueryUserPostsRequest{Author: ""})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("user with no posts", func(t *testing.T) {
		resp, err := qs.UserPosts(f.ctx, &types.QueryUserPostsRequest{Author: testCreator})
		require.NoError(t, err)
		require.Empty(t, resp.Posts)
	})

	t.Run("user with post", func(t *testing.T) {
		cat := f.createTestCategory(t, "Test Category")
		post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

		resp, err := qs.UserPosts(f.ctx, &types.QueryUserPostsRequest{Author: testCreator})
		require.NoError(t, err)
		require.NotEmpty(t, resp.Posts)
		found := false
		for _, p := range resp.Posts {
			if p.PostId == post.PostId {
				found = true
				require.Equal(t, cat.CategoryId, p.CategoryId)
				require.Equal(t, types.PostStatus_POST_STATUS_ACTIVE, p.Status)
			}
		}
		require.True(t, found, "created post not returned")
	})
}
