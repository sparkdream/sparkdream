package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryPinnedPosts(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.PinnedPosts(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no pinned posts", func(t *testing.T) {
		resp, err := qs.PinnedPosts(f.ctx, &types.QueryPinnedPostsRequest{})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.PostId)
	})

	t.Run("has pinned post", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		// Pin the post
		post.Pinned = true
		post.PinnedBy = authority
		post.PinnedAt = f.sdkCtx().BlockTime().Unix()
		post.PinPriority = 5
		f.keeper.Post.Set(f.ctx, post.PostId, post)
		require.NoError(t, f.keeper.PostsByPinned.Set(f.ctx, collections.Join(post.CategoryId, post.PostId)))

		resp, err := qs.PinnedPosts(f.ctx, &types.QueryPinnedPostsRequest{})
		require.NoError(t, err)
		require.Equal(t, post.PostId, resp.PostId)
		require.Equal(t, authority, resp.PinnedBy)
		require.Equal(t, uint64(5), resp.Priority)
	})

	t.Run("filter by category", func(t *testing.T) {
		cat := f.createTestCategory(t, "Test Category")
		post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

		// Pin the post
		post.Pinned = true
		post.PinnedBy = authority
		post.PinPriority = 3
		f.keeper.Post.Set(f.ctx, post.PostId, post)
		require.NoError(t, f.keeper.PostsByPinned.Set(f.ctx, collections.Join(post.CategoryId, post.PostId)))

		// Query with category filter
		resp, err := qs.PinnedPosts(f.ctx, &types.QueryPinnedPostsRequest{CategoryId: cat.CategoryId})
		require.NoError(t, err)
		require.Equal(t, post.PostId, resp.PostId)
	})

	t.Run("reply not returned (only root posts)", func(t *testing.T) {
		rootPost := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator, rootPost.PostId, 0)

		// Pin the reply (should not be returned)
		reply.Pinned = true
		reply.PinnedBy = authority
		f.keeper.Post.Set(f.ctx, reply.PostId, reply)

		resp, err := qs.PinnedPosts(f.ctx, &types.QueryPinnedPostsRequest{})
		require.NoError(t, err)
		// Should not return the pinned reply since it's not a root post
		require.NotEqual(t, reply.PostId, resp.PostId)
	})
}
