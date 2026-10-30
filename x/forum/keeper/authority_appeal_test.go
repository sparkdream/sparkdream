package keeper_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// Committee and council locks and moves write an authority record (no
// sentinel, no bond), so the thread author can appeal them through the forum's
// own handlers. An overturned appeal reverses the action, an upheld one keeps
// it, each action is appealable once and only by the thread author, and a
// timeout leaves the council's decision in place.

func TestAuthorityLockAppeal(t *testing.T) {
	setup := func(t *testing.T) (*fixture, uint64) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "General")
		thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)
		authority, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
		require.NoError(t, err)
		_, err = f.msgServer.LockThread(f.ctx, &types.MsgLockThread{
			Creator: authority, RootId: thread.PostId, Reason: "off-topic",
			Authority: types.ModerationAuthority_MODERATION_AUTHORITY_COUNCIL,
		})
		require.NoError(t, err)
		params, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)
		f.ctx = f.sdkCtx().WithBlockTime(f.sdkCtx().BlockTime().Add(time.Duration(params.LockAppealCooldown+1) * time.Second))
		return f, thread.PostId
	}
	target := func(id uint64) string { return fmt.Sprintf("%d", id) }
	lockType := reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK

	t.Run("only the author, only once", func(t *testing.T) {
		f, id := setup(t)
		_, err := f.msgServer.AppealThreadLock(f.ctx, &types.MsgAppealThreadLock{Creator: testCreator2, RootId: id})
		require.ErrorIs(t, err, types.ErrNotThreadAuthor)

		_, err = f.msgServer.AppealThreadLock(f.ctx, &types.MsgAppealThreadLock{Creator: testCreator, RootId: id})
		require.NoError(t, err)
		rec, err := f.keeper.ThreadLockRecord.Get(f.ctx, id)
		require.NoError(t, err)
		require.True(t, rec.AppealPending)
		require.Empty(t, rec.Sentinel)

		_, err = f.msgServer.AppealThreadLock(f.ctx, &types.MsgAppealThreadLock{Creator: testCreator, RootId: id})
		require.ErrorIs(t, err, types.ErrLockAppealAlreadyFiled)

		// No sentinel to slash or release.
		sentinel, err := f.keeper.GetActionSentinel(f.ctx, lockType, target(id))
		require.NoError(t, err)
		require.Empty(t, sentinel)
		committed, err := f.keeper.GetActionCommittedAmount(f.ctx, lockType, target(id))
		require.NoError(t, err)
		require.True(t, committed.IsZero())
	})

	t.Run("overturned unlocks", func(t *testing.T) {
		f, id := setup(t)
		_, err := f.msgServer.AppealThreadLock(f.ctx, &types.MsgAppealThreadLock{Creator: testCreator, RootId: id})
		require.NoError(t, err)
		require.NoError(t, f.keeper.ReverseSentinelAction(f.ctx, lockType, target(id)))
		require.NoError(t, f.keeper.OnAppealOutcome(f.ctx, lockType, target(id), reptypes.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED))
		post, err := f.keeper.Post.Get(f.ctx, id)
		require.NoError(t, err)
		require.False(t, post.Locked)
		_, err = f.keeper.ThreadLockRecord.Get(f.ctx, id)
		require.Error(t, err)
	})

	t.Run("upheld keeps the lock and ends the appeal path", func(t *testing.T) {
		f, id := setup(t)
		_, err := f.msgServer.AppealThreadLock(f.ctx, &types.MsgAppealThreadLock{Creator: testCreator, RootId: id})
		require.NoError(t, err)
		require.NoError(t, f.keeper.OnAppealOutcome(f.ctx, lockType, target(id), reptypes.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD))
		post, err := f.keeper.Post.Get(f.ctx, id)
		require.NoError(t, err)
		require.True(t, post.Locked)
		_, err = f.msgServer.AppealThreadLock(f.ctx, &types.MsgAppealThreadLock{Creator: testCreator, RootId: id})
		require.ErrorIs(t, err, types.ErrGovLockNotAppealable)
	})

	t.Run("timeout keeps the council lock", func(t *testing.T) {
		f, id := setup(t)
		_, err := f.msgServer.AppealThreadLock(f.ctx, &types.MsgAppealThreadLock{Creator: testCreator, RootId: id})
		require.NoError(t, err)
		require.NoError(t, f.keeper.OnAppealOutcome(f.ctx, lockType, target(id), reptypes.GovAppealStatus_GOV_APPEAL_STATUS_TIMEOUT))
		post, err := f.keeper.Post.Get(f.ctx, id)
		require.NoError(t, err)
		require.True(t, post.Locked)
		_, err = f.keeper.ThreadLockRecord.Get(f.ctx, id)
		require.Error(t, err)
	})

	t.Run("not a sentinel lock for a former committee member", func(t *testing.T) {
		f, id := setup(t)
		// The signer left the committee: it still matches LockedBy, but an
		// authority record is not a sentinel lock it may undo.
		post, err := f.keeper.Post.Get(f.ctx, id)
		require.NoError(t, err)
		post.LockedBy = testSentinel
		require.NoError(t, f.keeper.Post.Set(f.ctx, id, post))
		_, err = f.msgServer.UnlockThread(f.ctx, &types.MsgUnlockThread{Creator: testSentinel, RootId: id})
		require.ErrorIs(t, err, types.ErrUnauthorized)
	})

	t.Run("unappealed record expires and the lock stands", func(t *testing.T) {
		f, id := setup(t)
		rec, err := f.keeper.ThreadLockRecord.Get(f.ctx, id)
		require.NoError(t, err)
		require.NoError(t, f.keeper.ExpireSentinelActionRecords(f.ctx, rec.LockedAt+types.DefaultLockAppealDeadline))
		_, err = f.keeper.ThreadLockRecord.Get(f.ctx, id)
		require.Error(t, err)
		post, err := f.keeper.Post.Get(f.ctx, id)
		require.NoError(t, err)
		require.True(t, post.Locked)
	})
}

func TestAuthorityMoveAppeal(t *testing.T) {
	setup := func(t *testing.T) (*fixture, uint64, uint64, uint64) {
		f := initFixture(t)
		from := f.createTestCategory(t, "General")
		to := f.createTestCategory(t, "Elsewhere")
		thread := f.createTestPost(t, testCreator, 0, from.CategoryId)
		authority, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
		require.NoError(t, err)
		_, err = f.msgServer.MoveThread(f.ctx, &types.MsgMoveThread{
			Creator: authority, RootId: thread.PostId, NewCategoryId: to.CategoryId, Reason: "wrong board",
			Authority: types.ModerationAuthority_MODERATION_AUTHORITY_COUNCIL,
		})
		require.NoError(t, err)
		params, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)
		f.ctx = f.sdkCtx().WithBlockTime(f.sdkCtx().BlockTime().Add(time.Duration(params.MoveAppealCooldown+1) * time.Second))
		return f, thread.PostId, from.CategoryId, to.CategoryId
	}
	moveType := reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_MOVE

	t.Run("overturned moves the thread back", func(t *testing.T) {
		f, id, from, _ := setup(t)
		_, err := f.msgServer.AppealThreadMove(f.ctx, &types.MsgAppealThreadMove{Creator: testCreator2, RootId: id})
		require.ErrorIs(t, err, types.ErrNotThreadAuthor)
		_, err = f.msgServer.AppealThreadMove(f.ctx, &types.MsgAppealThreadMove{Creator: testCreator, RootId: id})
		require.NoError(t, err)
		_, err = f.msgServer.AppealThreadMove(f.ctx, &types.MsgAppealThreadMove{Creator: testCreator, RootId: id})
		require.ErrorIs(t, err, types.ErrMoveAppealAlreadyFiled)

		target := fmt.Sprintf("%d", id)
		require.NoError(t, f.keeper.ReverseSentinelAction(f.ctx, moveType, target))
		require.NoError(t, f.keeper.OnAppealOutcome(f.ctx, moveType, target, reptypes.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED))
		post, err := f.keeper.Post.Get(f.ctx, id)
		require.NoError(t, err)
		require.Equal(t, from, post.CategoryId)
	})

	t.Run("upheld keeps the move", func(t *testing.T) {
		f, id, _, to := setup(t)
		_, err := f.msgServer.AppealThreadMove(f.ctx, &types.MsgAppealThreadMove{Creator: testCreator, RootId: id})
		require.NoError(t, err)
		require.NoError(t, f.keeper.OnAppealOutcome(f.ctx, moveType, fmt.Sprintf("%d", id), reptypes.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD))
		post, err := f.keeper.Post.Get(f.ctx, id)
		require.NoError(t, err)
		require.Equal(t, to, post.CategoryId)
		_, err = f.msgServer.AppealThreadMove(f.ctx, &types.MsgAppealThreadMove{Creator: testCreator, RootId: id})
		require.ErrorIs(t, err, types.ErrGovLockNotAppealable)
	})
}
