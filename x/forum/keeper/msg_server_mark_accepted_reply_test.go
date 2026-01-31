package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestMsgMarkAcceptedReply(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	t.Run("invalid creator address", func(t *testing.T) {
		_, err := ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{
			Creator:  "invalid",
			ThreadId: 1,
			ReplyId:  2,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("thread not found", func(t *testing.T) {
		_, err := ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{
			Creator:  testCreator,
			ThreadId: 999,
			ReplyId:  2,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("not thread author", func(t *testing.T) {
		// Create thread
		thread := f.createTestPost(t, testCreator, 0, 0)

		// Create a reply in the thread
		reply := f.createTestPost(t, testCreator2, thread.PostId, thread.PostId)

		_, err := ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{
			Creator:  testCreator2, // Not the author
			ThreadId: thread.PostId,
			ReplyId:  reply.PostId,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "only thread author")
	})

	t.Run("reply not found", func(t *testing.T) {
		// Create thread
		thread := f.createTestPost(t, testCreator, 0, 0)

		_, err := ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{
			Creator:  testCreator,
			ThreadId: thread.PostId,
			ReplyId:  999,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("reply not in thread", func(t *testing.T) {
		// Create two threads
		thread1 := f.createTestPost(t, testCreator, 0, 0)
		thread2 := f.createTestPost(t, testCreator, 0, 0)

		// Create a reply in thread2
		reply := f.createTestPost(t, testCreator2, thread2.PostId, thread2.PostId)

		// Try to accept reply from thread2 in thread1
		_, err := ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{
			Creator:  testCreator,
			ThreadId: thread1.PostId,
			ReplyId:  reply.PostId,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not in the specified thread")
	})

	t.Run("cannot accept root post", func(t *testing.T) {
		// Create thread
		thread := f.createTestPost(t, testCreator, 0, 0)

		_, err := ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{
			Creator:  testCreator,
			ThreadId: thread.PostId,
			ReplyId:  thread.PostId, // The thread root itself
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot accept the thread root")
	})

	t.Run("already has accepted reply", func(t *testing.T) {
		// Create thread
		thread := f.createTestPost(t, testCreator, 0, 0)

		// Create replies
		reply1 := f.createTestPost(t, testCreator2, thread.PostId, thread.PostId)
		reply2 := f.createTestPost(t, testSentinel, thread.PostId, thread.PostId)

		now := f.sdkCtx().BlockTime().Unix()

		// Create metadata with already accepted reply
		metadata := types.ThreadMetadata{
			ThreadId:        thread.PostId,
			AcceptedReplyId: reply1.PostId,
			AcceptedBy:      testCreator,
			AcceptedAt:      now,
		}
		f.keeper.ThreadMetadata.Set(f.ctx, thread.PostId, metadata)

		_, err := ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{
			Creator:  testCreator,
			ThreadId: thread.PostId,
			ReplyId:  reply2.PostId,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "already accepted")
	})

	t.Run("success", func(t *testing.T) {
		// Create thread
		thread := f.createTestPost(t, testCreator, 0, 0)

		// Create reply
		reply := f.createTestPost(t, testCreator2, thread.PostId, thread.PostId)

		_, err := ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{
			Creator:  testCreator,
			ThreadId: thread.PostId,
			ReplyId:  reply.PostId,
		})
		require.NoError(t, err)

		// Verify reply was accepted
		metadata, err := f.keeper.ThreadMetadata.Get(f.ctx, thread.PostId)
		require.NoError(t, err)
		require.Equal(t, reply.PostId, metadata.AcceptedReplyId)
		require.Equal(t, testCreator, metadata.AcceptedBy)
		require.NotZero(t, metadata.AcceptedAt)
	})
}
