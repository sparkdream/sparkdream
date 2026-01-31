package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryThread(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.Thread(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("thread not found", func(t *testing.T) {
		_, err := qs.Thread(f.ctx, &types.QueryThreadRequest{RootId: 999})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("not a root post", func(t *testing.T) {
		// Create a root post
		rootPost := f.createTestPost(t, testCreator, 0, 0)

		// Create a reply (not a root post)
		reply := f.createTestPost(t, testCreator, rootPost.PostId, 0)

		_, err := qs.Thread(f.ctx, &types.QueryThreadRequest{RootId: reply.PostId})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("successful query", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		resp, err := qs.Thread(f.ctx, &types.QueryThreadRequest{RootId: post.PostId})
		require.NoError(t, err)
		require.Equal(t, post.PostId, resp.PostId)
		require.Equal(t, testCreator, resp.Author)
		require.Equal(t, uint64(0), resp.ParentId)
	})
}
