package keeper_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// TestReverseSentinelAction covers the privileged keeper method that x/rep
// invokes from MsgResolveGovActionAppeal's OVERTURNED branch. It rolls back
// the content-state effects of a sentinel hide / lock / move so the appeal
// loop completes (sentinel slashed AND content restored).
func TestReverseSentinelAction(t *testing.T) {
	t.Run("hide: post unhidden, HideRecord removed", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "ReverseHideCat")
		postID := f.hidePostViaSentinel(t, testSentinel, "2000000000", cat.CategoryId)

		// Sanity baseline.
		stored, err := f.keeper.Post.Get(f.ctx, postID)
		require.NoError(t, err)
		require.Equal(t, types.PostStatus_POST_STATUS_HIDDEN, stored.Status)
		_, err = f.keeper.HideRecord.Get(f.ctx, postID)
		require.NoError(t, err)

		// Use UNSPECIFIED to hit the default (hide) branch — same dispatch
		// the appeal resolver uses for hide-appeal verdicts today.
		require.NoError(t, f.keeper.ReverseSentinelAction(
			f.ctx,
			reptypes.GovActionType_GOV_ACTION_TYPE_UNSPECIFIED,
			strconv.FormatUint(postID, 10),
		))

		restored, err := f.keeper.Post.Get(f.ctx, postID)
		require.NoError(t, err)
		require.Equal(t, types.PostStatus_POST_STATUS_ACTIVE, restored.Status)
		require.Empty(t, restored.HiddenBy)
		require.EqualValues(t, 0, restored.HiddenAt)

		_, err = f.keeper.HideRecord.Get(f.ctx, postID)
		require.Error(t, err, "HideRecord must be removed after reverse")
	})

	t.Run("hide: skip when parent category deleted (dangling guard)", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "ReverseHideCategoryGone")
		postID := f.hidePostViaSentinel(t, testSentinel, "2000000000", cat.CategoryId)

		// Simulate category deletion mid-life.
		delete(f.commonsKeeper.categories, cat.CategoryId)

		require.NoError(t, f.keeper.ReverseSentinelAction(
			f.ctx,
			reptypes.GovActionType_GOV_ACTION_TYPE_UNSPECIFIED,
			strconv.FormatUint(postID, 10),
		), "soft skip — appeal resolver continues")

		// Post must remain HIDDEN (dangling reference avoided).
		post, err := f.keeper.Post.Get(f.ctx, postID)
		require.NoError(t, err)
		require.Equal(t, types.PostStatus_POST_STATUS_HIDDEN, post.Status,
			"reverse must NOT flip status into a deleted category")
	})

	t.Run("hide: idempotent on already-unhidden post", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "ReverseHideIdempotent")
		postID := f.hidePostViaSentinel(t, testSentinel, "2000000000", cat.CategoryId)

		// Sentinel self-corrects first (real user path).
		_, err := f.msgServer.UnhidePost(f.ctx, &types.MsgUnhidePost{
			Creator: testSentinel,
			PostId:  postID,
		})
		require.NoError(t, err)

		// Now appeal lands OVERTURNED — reverse is invoked but the post is
		// already ACTIVE. Must not error and must not re-mutate.
		require.NoError(t, f.keeper.ReverseSentinelAction(
			f.ctx,
			reptypes.GovActionType_GOV_ACTION_TYPE_UNSPECIFIED,
			strconv.FormatUint(postID, 10),
		))

		post, err := f.keeper.Post.Get(f.ctx, postID)
		require.NoError(t, err)
		require.Equal(t, types.PostStatus_POST_STATUS_ACTIVE, post.Status)
	})

	t.Run("hide: missing post is soft skip", func(t *testing.T) {
		f := initFixture(t)
		// No state setup — pure soft-skip path.
		require.NoError(t, f.keeper.ReverseSentinelAction(
			f.ctx,
			reptypes.GovActionType_GOV_ACTION_TYPE_UNSPECIFIED,
			"99999999",
		))
	})

	t.Run("lock: thread unlocked, ThreadLockRecord removed", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "ReverseLockCat")
		root := f.createTestPost(t, testCreator, 0, cat.CategoryId)

		// Inject locked state directly — avoids re-creating the full sentinel
		// lock plumbing in this unit test. The reverse path doesn't care how
		// the lock got there.
		p, err := f.keeper.Post.Get(f.ctx, root.PostId)
		require.NoError(t, err)
		p.Locked = true
		p.LockedBy = testSentinel
		p.LockedAt = 1000
		p.LockReason = "test lock"
		require.NoError(t, f.keeper.Post.Set(f.ctx, root.PostId, p))
		require.NoError(t, f.keeper.ThreadLockRecord.Set(f.ctx, root.PostId, types.ThreadLockRecord{
			RootId:   root.PostId,
			Sentinel: testSentinel,
			LockedAt: 1000,
		}))

		require.NoError(t, f.keeper.ReverseSentinelAction(
			f.ctx,
			reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK,
			strconv.FormatUint(root.PostId, 10),
		))

		restored, err := f.keeper.Post.Get(f.ctx, root.PostId)
		require.NoError(t, err)
		require.False(t, restored.Locked, "Locked flag must be cleared")
		require.Empty(t, restored.LockedBy)
		require.EqualValues(t, 0, restored.LockedAt)
		require.Empty(t, restored.LockReason)

		_, err = f.keeper.ThreadLockRecord.Get(f.ctx, root.PostId)
		require.Error(t, err, "ThreadLockRecord must be removed after reverse")
	})

	t.Run("lock: bypasses LockAppealDeadline that MsgUnlockThread enforces", func(t *testing.T) {
		// Regression guard: a sentinel calling MsgUnlockThread after the
		// appeal deadline gets ErrLockAppealExpired. The privileged reverse
		// path (driven by an appeal resolution) must NOT honor that deadline —
		// it always succeeds. Otherwise overturned appeals on old locks would
		// silently fail to restore the thread.
		f := initFixture(t)
		cat := f.createTestCategory(t, "ReverseLockBypassDeadline")
		root := f.createTestPost(t, testCreator, 0, cat.CategoryId)

		p, err := f.keeper.Post.Get(f.ctx, root.PostId)
		require.NoError(t, err)
		p.Locked = true
		p.LockedBy = testSentinel
		// Lock from a long time ago so any deadline-based path would refuse.
		p.LockedAt = 1
		require.NoError(t, f.keeper.Post.Set(f.ctx, root.PostId, p))
		require.NoError(t, f.keeper.ThreadLockRecord.Set(f.ctx, root.PostId, types.ThreadLockRecord{
			RootId:   root.PostId,
			Sentinel: testSentinel,
			LockedAt: 1,
		}))

		require.NoError(t, f.keeper.ReverseSentinelAction(
			f.ctx,
			reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK,
			strconv.FormatUint(root.PostId, 10),
		))

		restored, _ := f.keeper.Post.Get(f.ctx, root.PostId)
		require.False(t, restored.Locked)
	})

	t.Run("move: thread restored to OriginalCategoryId, ThreadMoveRecord removed", func(t *testing.T) {
		f := initFixture(t)
		origCat := f.createTestCategory(t, "MoveOriginal")
		newCat := f.createTestCategory(t, "MoveNew")
		root := f.createTestPost(t, testCreator, 0, newCat.CategoryId)

		// Seed the "moved" state directly.
		require.NoError(t, f.keeper.ThreadMoveRecord.Set(f.ctx, root.PostId, types.ThreadMoveRecord{
			RootId:             root.PostId,
			Sentinel:           testSentinel,
			OriginalCategoryId: origCat.CategoryId,
			NewCategoryId:      newCat.CategoryId,
			MovedAt:            1000,
		}))

		require.NoError(t, f.keeper.ReverseSentinelAction(
			f.ctx,
			reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_MOVE,
			strconv.FormatUint(root.PostId, 10),
		))

		restored, err := f.keeper.Post.Get(f.ctx, root.PostId)
		require.NoError(t, err)
		require.Equal(t, origCat.CategoryId, restored.CategoryId,
			"CategoryId must be restored to OriginalCategoryId")

		_, err = f.keeper.ThreadMoveRecord.Get(f.ctx, root.PostId)
		require.Error(t, err, "ThreadMoveRecord must be removed after reverse")
	})

	t.Run("move: skip when OriginalCategoryId has been deleted", func(t *testing.T) {
		f := initFixture(t)
		origCat := f.createTestCategory(t, "MoveOrigDeleted")
		newCat := f.createTestCategory(t, "MoveNewSurvives")
		root := f.createTestPost(t, testCreator, 0, newCat.CategoryId)

		require.NoError(t, f.keeper.ThreadMoveRecord.Set(f.ctx, root.PostId, types.ThreadMoveRecord{
			RootId:             root.PostId,
			Sentinel:           testSentinel,
			OriginalCategoryId: origCat.CategoryId,
			NewCategoryId:      newCat.CategoryId,
		}))

		// Original category deleted while thread sat in the new one.
		delete(f.commonsKeeper.categories, origCat.CategoryId)

		require.NoError(t, f.keeper.ReverseSentinelAction(
			f.ctx,
			reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_MOVE,
			strconv.FormatUint(root.PostId, 10),
		), "soft skip")

		// Thread stays in newCat (no dangling reference).
		restored, err := f.keeper.Post.Get(f.ctx, root.PostId)
		require.NoError(t, err)
		require.Equal(t, newCat.CategoryId, restored.CategoryId,
			"reverse must NOT relocate into a deleted category")
	})

	t.Run("invalid action target", func(t *testing.T) {
		f := initFixture(t)
		err := f.keeper.ReverseSentinelAction(
			f.ctx,
			reptypes.GovActionType_GOV_ACTION_TYPE_UNSPECIFIED,
			"not-a-number",
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid action target")
	})
}
