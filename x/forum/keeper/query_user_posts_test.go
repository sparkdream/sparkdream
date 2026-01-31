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
		require.Equal(t, uint64(0), resp.PostId)
	})

	t.Run("user with post", func(t *testing.T) {
		cat := f.createTestCategory(t, "Test Category")
		post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

		resp, err := qs.UserPosts(f.ctx, &types.QueryUserPostsRequest{Author: testCreator})
		require.NoError(t, err)
		require.Equal(t, post.PostId, resp.PostId)
		require.Equal(t, cat.CategoryId, resp.CategoryId)
		require.Equal(t, uint64(types.PostStatus_POST_STATUS_ACTIVE), resp.Status)
	})
}
