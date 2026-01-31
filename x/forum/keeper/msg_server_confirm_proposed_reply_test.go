package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestMsgConfirmProposedReply(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	t.Run("invalid creator address", func(t *testing.T) {
		_, err := ms.ConfirmProposedReply(f.ctx, &types.MsgConfirmProposedReply{
			Creator:  "invalid",
			ThreadId: 1,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("thread not found", func(t *testing.T) {
		_, err := ms.ConfirmProposedReply(f.ctx, &types.MsgConfirmProposedReply{
			Creator:  testCreator,
			ThreadId: 999,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("not thread author", func(t *testing.T) {
		// Create thread
		thread := f.createTestPost(t, testCreator, 0, 0)

		// Create metadata with proposed reply
		metadata := types.ThreadMetadata{
			ThreadId:        thread.PostId,
			ProposedReplyId: 100,
			ProposedBy:      testSentinel,
		}
		f.keeper.ThreadMetadata.Set(f.ctx, thread.PostId, metadata)

		_, err := ms.ConfirmProposedReply(f.ctx, &types.MsgConfirmProposedReply{
			Creator:  testCreator2, // Not the author
			ThreadId: thread.PostId,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "only thread author")
	})

	t.Run("no proposed reply", func(t *testing.T) {
		// Create thread
		thread := f.createTestPost(t, testCreator, 0, 0)

		// Create metadata without proposed reply
		metadata := types.ThreadMetadata{
			ThreadId: thread.PostId,
		}
		f.keeper.ThreadMetadata.Set(f.ctx, thread.PostId, metadata)

		_, err := ms.ConfirmProposedReply(f.ctx, &types.MsgConfirmProposedReply{
			Creator:  testCreator,
			ThreadId: thread.PostId,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "no proposed reply")
	})

	t.Run("already has accepted reply", func(t *testing.T) {
		// Create thread
		thread := f.createTestPost(t, testCreator, 0, 0)

		// Create metadata with both proposed and accepted
		metadata := types.ThreadMetadata{
			ThreadId:        thread.PostId,
			ProposedReplyId: 100,
			ProposedBy:      testSentinel,
			AcceptedReplyId: 50, // Already accepted
		}
		f.keeper.ThreadMetadata.Set(f.ctx, thread.PostId, metadata)

		_, err := ms.ConfirmProposedReply(f.ctx, &types.MsgConfirmProposedReply{
			Creator:  testCreator,
			ThreadId: thread.PostId,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "already accepted")
	})

	t.Run("success", func(t *testing.T) {
		// Create thread
		thread := f.createTestPost(t, testCreator, 0, 0)

		now := f.sdkCtx().BlockTime().Unix()

		// Create metadata with proposed reply
		metadata := types.ThreadMetadata{
			ThreadId:        thread.PostId,
			ProposedReplyId: 100,
			ProposedBy:      testSentinel,
			ProposedAt:      now,
		}
		f.keeper.ThreadMetadata.Set(f.ctx, thread.PostId, metadata)

		_, err := ms.ConfirmProposedReply(f.ctx, &types.MsgConfirmProposedReply{
			Creator:  testCreator,
			ThreadId: thread.PostId,
		})
		require.NoError(t, err)

		// Verify proposed moved to accepted
		updated, err := f.keeper.ThreadMetadata.Get(f.ctx, thread.PostId)
		require.NoError(t, err)
		require.Equal(t, uint64(100), updated.AcceptedReplyId)
		require.Equal(t, testCreator, updated.AcceptedBy)
		require.Equal(t, uint64(0), updated.ProposedReplyId)
		require.Empty(t, updated.ProposedBy)
	})
}
