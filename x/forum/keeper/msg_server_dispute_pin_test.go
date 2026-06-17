package keeper_test

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
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
					PostId:        100,
					PinnedBy:      testSentinel,
					PinnedAt:      now,
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
					PostId:        100,
					PinnedBy:      testSentinel,
					PinnedAt:      now,
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
		// Capture the gov-action-appeal routing args (ActionType REPLY_PIN,
		// ActionTarget = reply id) so a dispute is provably folded into the
		// unified appeal path rather than a bare initiative.
		var gotType reptypes.GovActionType
		var gotTarget string
		f.repKeeper.CreateGovActionAppealFn = func(_ context.Context, at reptypes.GovActionType, target string, _ sdk.AccAddress, _ string) (uint64, uint64, error) {
			gotType = at
			gotTarget = target
			return 9, 9, nil
		}

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

		// Verify the dispute was folded into the unified gov-action-appeal path
		// with the correct action type + target (the reply id).
		require.Equal(t, reptypes.GovActionType_GOV_ACTION_TYPE_REPLY_PIN, gotType)
		require.Equal(t, "200", gotTarget)

		// Verify pin marked as disputed, carrying the returned initiative id.
		updated, err := f.keeper.ThreadMetadata.Get(f.ctx, thread.PostId)
		require.NoError(t, err)
		require.Len(t, updated.PinnedRecords, 1)
		require.True(t, updated.PinnedRecords[0].Disputed)
		require.Equal(t, uint64(9), updated.PinnedRecords[0].InitiativeId)
	})
}
