package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryLockedThreads(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.LockedThreads(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no locked threads", func(t *testing.T) {
		resp, err := qs.LockedThreads(f.ctx, &types.QueryLockedThreadsRequest{})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.RootId)
	})

	t.Run("has locked thread", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		// Lock the thread
		post.Locked = true
		post.LockedBy = testSentinel
		post.LockedAt = f.sdkCtx().BlockTime().Unix()
		post.LockReason = "Test lock"
		f.keeper.Post.Set(f.ctx, post.PostId, post)

		resp, err := qs.LockedThreads(f.ctx, &types.QueryLockedThreadsRequest{})
		require.NoError(t, err)
		require.Equal(t, post.PostId, resp.RootId)
		require.Equal(t, testSentinel, resp.LockedBy)
		require.Equal(t, post.LockedAt, resp.LockedAt)
	})

	t.Run("reply not returned (only root posts)", func(t *testing.T) {
		rootPost := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator, rootPost.PostId, 0)

		// Lock the reply (should not be returned)
		reply.Locked = true
		reply.LockedBy = testSentinel
		reply.LockedAt = f.sdkCtx().BlockTime().Unix()
		f.keeper.Post.Set(f.ctx, reply.PostId, reply)

		resp, err := qs.LockedThreads(f.ctx, &types.QueryLockedThreadsRequest{})
		require.NoError(t, err)
		// Should not return the locked reply since it's not a root post
		require.NotEqual(t, reply.PostId, resp.RootId)
	})
}
