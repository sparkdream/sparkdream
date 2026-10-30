package keeper_test

import (
	"testing"

	"sparkdream/x/forum/types"

	"github.com/stretchr/testify/require"
)

// Locking, moving or following a thread needs an ACTIVE root: moderating a
// hidden, deleted or archived thread would reserve sentinel bond and open an
// appealable record on dead content.
func TestThreadOpsRequireActiveRoot(t *testing.T) {
	for _, status := range []types.PostStatus{
		types.PostStatus_POST_STATUS_HIDDEN,
		types.PostStatus_POST_STATUS_DELETED,
		types.PostStatus_POST_STATUS_ARCHIVED,
	} {
		t.Run(status.String(), func(t *testing.T) {
			f := initFixture(t)
			cat := f.createTestCategory(t, "General")
			cat2 := f.createTestCategory(t, "Off-Topic")
			thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)
			f.createTestSentinel(t, testSentinel, "3000000000")

			post, err := f.keeper.Post.Get(f.ctx, thread.PostId)
			require.NoError(t, err)
			post.Status = status
			require.NoError(t, f.keeper.Post.Set(f.ctx, thread.PostId, post))

			_, err = f.msgServer.LockThread(f.ctx, &types.MsgLockThread{Creator: testSentinel, RootId: thread.PostId, Reason: "off-topic"})
			require.ErrorIs(t, err, types.ErrThreadNotActive, "lock")
			_, err = f.msgServer.MoveThread(f.ctx, &types.MsgMoveThread{Creator: testSentinel, RootId: thread.PostId, NewCategoryId: cat2.CategoryId, Reason: "better fit"})
			require.ErrorIs(t, err, types.ErrThreadNotActive, "move")
			_, err = f.msgServer.FollowThread(f.ctx, &types.MsgFollowThread{Creator: testCreator2, ThreadId: thread.PostId})
			require.ErrorIs(t, err, types.ErrThreadNotActive, "follow")
		})
	}
}

// A bounty under moderation is refused with ErrBountyInModeration (the
// check used to sit after the ACTIVE requirement and could never fire).
func TestAssignBountyInModerationError(t *testing.T) {
	f := initFixture(t)
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	reply := f.createTestPost(t, testCreator2, thread.PostId, cat.CategoryId)
	bounty := f.createTestBounty(t, testCreator, thread.PostId, "100000000")

	b, err := f.keeper.Bounty.Get(f.ctx, bounty.Id)
	require.NoError(t, err)
	b.Status = types.BountyStatus_BOUNTY_STATUS_MODERATION_PENDING
	require.NoError(t, f.keeper.Bounty.Set(f.ctx, bounty.Id, b))

	_, err = f.msgServer.AssignBountyToReply(f.ctx, &types.MsgAssignBountyToReply{
		Creator: testCreator, ThreadId: thread.PostId, ReplyId: reply.PostId, Reason: "great answer",
	})
	require.ErrorIs(t, err, types.ErrBountyInModeration)
}
