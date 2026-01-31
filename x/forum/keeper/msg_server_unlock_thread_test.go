package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestMsgServerUnlockThread(t *testing.T) {
	f := initFixture(t)
	authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	t.Run("invalid creator address", func(t *testing.T) {
		msg := &types.MsgUnlockThread{
			Creator: "invalid",
			RootId:  1,
		}
		_, err := f.msgServer.UnlockThread(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("post not found", func(t *testing.T) {
		msg := &types.MsgUnlockThread{
			Creator: testCreator,
			RootId:  999,
		}
		_, err := f.msgServer.UnlockThread(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrPostNotFound)
	})

	t.Run("thread not locked", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		msg := &types.MsgUnlockThread{
			Creator: authority,
			RootId:  post.PostId,
		}
		_, err := f.msgServer.UnlockThread(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrThreadNotLocked)
	})

	t.Run("gov authority can unlock any thread", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		// Lock the thread
		post.Locked = true
		post.LockedBy = testSentinel
		post.LockedAt = f.sdkCtx().BlockTime().Unix()
		post.LockReason = "Test lock"
		f.keeper.Post.Set(f.ctx, post.PostId, post)

		msg := &types.MsgUnlockThread{
			Creator: authority,
			RootId:  post.PostId,
		}
		_, err := f.msgServer.UnlockThread(f.ctx, msg)
		require.NoError(t, err)

		// Verify unlocked
		updatedPost, err := f.keeper.Post.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.False(t, updatedPost.Locked)
		require.Empty(t, updatedPost.LockedBy)
	})

	t.Run("sentinel cannot unlock thread they did not lock", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		// Lock by different sentinel
		post.Locked = true
		post.LockedBy = testCreator2
		post.LockedAt = f.sdkCtx().BlockTime().Unix()
		f.keeper.Post.Set(f.ctx, post.PostId, post)

		msg := &types.MsgUnlockThread{
			Creator: testSentinel,
			RootId:  post.PostId,
		}
		_, err := f.msgServer.UnlockThread(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "sentinels can only unlock threads they locked")
	})

	t.Run("sentinel can unlock thread they locked", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		// Lock the thread by the sentinel
		post.Locked = true
		post.LockedBy = testSentinel
		post.LockedAt = f.sdkCtx().BlockTime().Unix()
		f.keeper.Post.Set(f.ctx, post.PostId, post)

		// Create lock record
		lockRecord := types.ThreadLockRecord{
			RootId:        post.PostId,
			Sentinel:      testSentinel,
			LockedAt:      f.sdkCtx().BlockTime().Unix(),
			AppealPending: false,
		}
		f.keeper.ThreadLockRecord.Set(f.ctx, post.PostId, lockRecord)

		msg := &types.MsgUnlockThread{
			Creator: testSentinel,
			RootId:  post.PostId,
		}
		_, err := f.msgServer.UnlockThread(f.ctx, msg)
		require.NoError(t, err)

		// Verify unlocked
		updatedPost, err := f.keeper.Post.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.False(t, updatedPost.Locked)
	})
}
