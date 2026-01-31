package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryThreadLockStatus(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.ThreadLockStatus(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("zero root_id", func(t *testing.T) {
		_, err := qs.ThreadLockStatus(f.ctx, &types.QueryThreadLockStatusRequest{RootId: 0})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("thread not found", func(t *testing.T) {
		_, err := qs.ThreadLockStatus(f.ctx, &types.QueryThreadLockStatusRequest{RootId: 999})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("not a root post", func(t *testing.T) {
		rootPost := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator, rootPost.PostId, 0)

		_, err := qs.ThreadLockStatus(f.ctx, &types.QueryThreadLockStatusRequest{RootId: reply.PostId})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("unlocked thread", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		resp, err := qs.ThreadLockStatus(f.ctx, &types.QueryThreadLockStatusRequest{RootId: post.PostId})
		require.NoError(t, err)
		require.False(t, resp.Locked)
		require.Empty(t, resp.LockedBy)
		require.False(t, resp.IsSentinelLock)
	})

	t.Run("locked by governance", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

		// Lock thread by governance
		post.Locked = true
		post.LockedBy = authority
		post.LockReason = "Governance lock"
		f.keeper.Post.Set(f.ctx, post.PostId, post)

		resp, err := qs.ThreadLockStatus(f.ctx, &types.QueryThreadLockStatusRequest{RootId: post.PostId})
		require.NoError(t, err)
		require.True(t, resp.Locked)
		require.Equal(t, authority, resp.LockedBy)
		require.Equal(t, "Governance lock", resp.Reason)
		require.False(t, resp.IsSentinelLock)
	})

	t.Run("locked by sentinel", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		// Lock thread by sentinel
		post.Locked = true
		post.LockedBy = testSentinel
		post.LockReason = "Sentinel lock"
		f.keeper.Post.Set(f.ctx, post.PostId, post)

		// Create sentinel lock record
		lockRecord := types.ThreadLockRecord{
			RootId:   post.PostId,
			Sentinel: testSentinel,
			LockedAt: f.sdkCtx().BlockTime().Unix(),
		}
		f.keeper.ThreadLockRecord.Set(f.ctx, post.PostId, lockRecord)

		resp, err := qs.ThreadLockStatus(f.ctx, &types.QueryThreadLockStatusRequest{RootId: post.PostId})
		require.NoError(t, err)
		require.True(t, resp.Locked)
		require.Equal(t, testSentinel, resp.LockedBy)
		require.True(t, resp.IsSentinelLock)
	})
}
