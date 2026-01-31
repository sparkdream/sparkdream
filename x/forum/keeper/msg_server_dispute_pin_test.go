package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestMsgDisputePin(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	t.Run("invalid creator address", func(t *testing.T) {
		_, err := ms.DisputePin(f.ctx, &types.MsgDisputePin{
			Creator:  "invalid",
			ThreadId: 1,
			ReplyId:  2,
			Reason:   "unfair pin",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("thread not found", func(t *testing.T) {
		_, err := ms.DisputePin(f.ctx, &types.MsgDisputePin{
			Creator:  testCreator,
			ThreadId: 999,
			ReplyId:  2,
			Reason:   "unfair pin",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("not thread author", func(t *testing.T) {
		// Create thread
		thread := f.createTestPost(t, testCreator, 0, 0)

		now := f.sdkCtx().BlockTime().Unix()

		// Create metadata with pinned record
		metadata := types.ThreadMetadata{
			ThreadId: thread.PostId,
			PinnedRecords: []*types.PinnedReplyRecord{
				{
					PostId:       100,
					PinnedBy:     testSentinel,
					PinnedAt:     now,
					IsSentinelPin: true,
				},
			},
		}
		f.keeper.ThreadMetadata.Set(f.ctx, thread.PostId, metadata)

		_, err := ms.DisputePin(f.ctx, &types.MsgDisputePin{
			Creator:  testCreator2, // Not the author
			ThreadId: thread.PostId,
			ReplyId:  100,
			Reason:   "unfair pin",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "only thread author")
	})

	t.Run("reply not pinned", func(t *testing.T) {
		// Create thread
		thread := f.createTestPost(t, testCreator, 0, 0)

		// Create metadata without pinned records
		metadata := types.ThreadMetadata{
			ThreadId:      thread.PostId,
			PinnedRecords: []*types.PinnedReplyRecord{},
		}
		f.keeper.ThreadMetadata.Set(f.ctx, thread.PostId, metadata)

		_, err := ms.DisputePin(f.ctx, &types.MsgDisputePin{
			Creator:  testCreator,
			ThreadId: thread.PostId,
			ReplyId:  100,
			Reason:   "unfair pin",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not pinned")
	})

	t.Run("cannot dispute gov pin", func(t *testing.T) {
		// Create thread
		thread := f.createTestPost(t, testCreator, 0, 0)

		now := f.sdkCtx().BlockTime().Unix()

		// Create metadata with gov pin (not sentinel)
		metadata := types.ThreadMetadata{
			ThreadId: thread.PostId,
			PinnedRecords: []*types.PinnedReplyRecord{
				{
					PostId:       100,
					PinnedBy:     testSentinel,
					PinnedAt:     now,
					IsSentinelPin: false, // Gov pin
				},
			},
		}
		f.keeper.ThreadMetadata.Set(f.ctx, thread.PostId, metadata)

		_, err := ms.DisputePin(f.ctx, &types.MsgDisputePin{
			Creator:  testCreator,
			ThreadId: thread.PostId,
			ReplyId:  100,
			Reason:   "unfair pin",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot dispute governance")
	})

	t.Run("already disputed", func(t *testing.T) {
		// Create thread
		thread := f.createTestPost(t, testCreator, 0, 0)

		now := f.sdkCtx().BlockTime().Unix()

		// Create metadata with already disputed pin
		metadata := types.ThreadMetadata{
			ThreadId: thread.PostId,
			PinnedRecords: []*types.PinnedReplyRecord{
				{
					PostId:        100,
					PinnedBy:      testSentinel,
					PinnedAt:      now,
					IsSentinelPin: true,
					Disputed:      true,
				},
			},
		}
		f.keeper.ThreadMetadata.Set(f.ctx, thread.PostId, metadata)

		_, err := ms.DisputePin(f.ctx, &types.MsgDisputePin{
			Creator:  testCreator,
			ThreadId: thread.PostId,
			ReplyId:  100,
			Reason:   "unfair pin",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "already disputed")
	})

	t.Run("success", func(t *testing.T) {
		// Create thread
		thread := f.createTestPost(t, testCreator, 0, 0)

		now := f.sdkCtx().BlockTime().Unix()

		// Create metadata with sentinel pin
		metadata := types.ThreadMetadata{
			ThreadId: thread.PostId,
			PinnedRecords: []*types.PinnedReplyRecord{
				{
					PostId:        200,
					PinnedBy:      testSentinel,
					PinnedAt:      now,
					IsSentinelPin: true,
				},
			},
		}
		f.keeper.ThreadMetadata.Set(f.ctx, thread.PostId, metadata)

		_, err := ms.DisputePin(f.ctx, &types.MsgDisputePin{
			Creator:  testCreator,
			ThreadId: thread.PostId,
			ReplyId:  200,
			Reason:   "biased pinning",
		})
		require.NoError(t, err)

		// Verify pin marked as disputed
		updated, err := f.keeper.ThreadMetadata.Get(f.ctx, thread.PostId)
		require.NoError(t, err)
		require.Len(t, updated.PinnedRecords, 1)
		require.True(t, updated.PinnedRecords[0].Disputed)
		require.NotZero(t, updated.PinnedRecords[0].InitiativeId)
	})
}
