package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// TestSentinelCooldownGatesModeration pins the shared overturn cooldown
// (rep-owned RoleActivity.overturn_cooldown_until) against every forum
// moderation entry point that consumes it: hide, lock, and move must all
// refuse with ErrSentinelCooldown while the cooldown is active — regardless
// of which surface's lost appeal started it (the record is shared with
// collect).
func TestSentinelCooldownGatesModeration(t *testing.T) {
	setup := func(t *testing.T) (*fixture, uint64, uint64) {
		t.Helper()
		f := initFixture(t)
		f.ctx = f.sdkCtx().WithBlockTime(time.Unix(1_000_000, 0))
		cat1 := f.createTestCategory(t, "General")
		cat2 := f.createTestCategory(t, "Off-Topic")
		thread := f.createTestPost(t, testCreator, 0, cat1.CategoryId)
		// Ample bond so the lock rep-tier/bond floors are not the failure.
		f.createTestSentinel(t, testSentinel, "3000000000")

		// Start the cooldown the way production does: an overturned verdict
		// on a moderation kind (a collect hide here, proving the cooldown is
		// cross-surface). The mock mirrors rep's kind policy.
		require.NoError(t, f.repKeeper.RecordRoleOutcome(f.ctx,
			reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, testSentinel,
			reptypes.ActionKindCollectHide, false))
		require.Greater(t,
			f.repKeeper.RoleOverturnCooldownUntil(f.ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, testSentinel),
			f.sdkCtx().BlockTime().Unix())
		return f, thread.PostId, cat2.CategoryId
	}

	t.Run("hide refused during cooldown", func(t *testing.T) {
		f, threadID, _ := setup(t)
		_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
			Creator:    testSentinel,
			PostId:     threadID,
			ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
			ReasonText: "Test",
		})
		require.ErrorIs(t, err, types.ErrSentinelCooldown)
	})

	t.Run("lock refused during cooldown", func(t *testing.T) {
		f, threadID, _ := setup(t)
		_, err := f.msgServer.LockThread(f.ctx, &types.MsgLockThread{
			Creator: testSentinel,
			RootId:  threadID,
			Reason:  "off-topic",
		})
		require.ErrorIs(t, err, types.ErrSentinelCooldown)
	})

	t.Run("move refused during cooldown", func(t *testing.T) {
		f, threadID, cat2 := setup(t)
		_, err := f.msgServer.MoveThread(f.ctx, &types.MsgMoveThread{
			Creator:       testSentinel,
			RootId:        threadID,
			NewCategoryId: cat2,
			Reason:        "wrong category",
		})
		require.ErrorIs(t, err, types.ErrSentinelCooldown)
	})

	t.Run("expired cooldown no longer gates", func(t *testing.T) {
		f, threadID, _ := setup(t)
		// Advance block time past the 24h cooldown.
		f.ctx = f.sdkCtx().WithBlockTime(
			time.Unix(1_000_000+reptypes.DefaultRoleOverturnCooldown+1, 0))
		_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
			Creator:    testSentinel,
			PostId:     threadID,
			ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
			ReasonText: "Test",
		})
		require.NoError(t, err)
	})
}
