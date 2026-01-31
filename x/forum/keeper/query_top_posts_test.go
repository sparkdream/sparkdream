package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryTopPosts(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.TopPosts(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no posts", func(t *testing.T) {
		resp, err := qs.TopPosts(f.ctx, &types.QueryTopPostsRequest{})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.PostId)
	})

	t.Run("returns highest upvoted post", func(t *testing.T) {
		// Create posts with different upvote counts
		post1 := f.createTestPost(t, testCreator, 0, 0)
		post2 := f.createTestPost(t, testCreator, 0, 0)
		post3 := f.createTestPost(t, testCreator, 0, 0)

		// Set upvote counts
		post1.UpvoteCount = 10
		f.keeper.Post.Set(f.ctx, post1.PostId, post1)

		post2.UpvoteCount = 50 // Highest
		f.keeper.Post.Set(f.ctx, post2.PostId, post2)

		post3.UpvoteCount = 25
		f.keeper.Post.Set(f.ctx, post3.PostId, post3)

		resp, err := qs.TopPosts(f.ctx, &types.QueryTopPostsRequest{})
		require.NoError(t, err)
		require.Equal(t, post2.PostId, resp.PostId)
		require.Equal(t, uint64(50), resp.UpvoteCount)
	})

	t.Run("ignores replies (only root posts)", func(t *testing.T) {
		rootPost := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator, rootPost.PostId, 0)

		// Give reply higher upvotes
		reply.UpvoteCount = 100
		f.keeper.Post.Set(f.ctx, reply.PostId, reply)

		rootPost.UpvoteCount = 5
		f.keeper.Post.Set(f.ctx, rootPost.PostId, rootPost)

		resp, err := qs.TopPosts(f.ctx, &types.QueryTopPostsRequest{})
		require.NoError(t, err)
		// Should not return the reply even though it has more upvotes
		require.NotEqual(t, reply.PostId, resp.PostId)
	})

	t.Run("ignores deleted posts", func(t *testing.T) {
		activePost := f.createTestPost(t, testCreator, 0, 0)
		deletedPost := f.createTestPost(t, testCreator, 0, 0)

		activePost.UpvoteCount = 5
		f.keeper.Post.Set(f.ctx, activePost.PostId, activePost)

		deletedPost.UpvoteCount = 100
		deletedPost.Status = types.PostStatus_POST_STATUS_DELETED
		f.keeper.Post.Set(f.ctx, deletedPost.PostId, deletedPost)

		resp, err := qs.TopPosts(f.ctx, &types.QueryTopPostsRequest{})
		require.NoError(t, err)
		require.NotEqual(t, deletedPost.PostId, resp.PostId)
	})
}
