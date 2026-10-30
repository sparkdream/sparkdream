package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"

	"github.com/stretchr/testify/require"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/forum/types"
)

// FRM-28: a sentinel's hide, lock and move are rewarded actions, so a
// sentinel cannot take them against its own content; it can against
// someone else's.
func TestSentinelCannotModerateOwnContent(t *testing.T) {
	f := initFixture(t)
	f.createTestSentinel(t, testSentinel, "5000000000")
	from := f.createTestCategory(t, "General")
	to := f.createTestCategory(t, "Elsewhere")
	own := f.createTestPost(t, testSentinel, 0, from.CategoryId)
	other := f.createTestPost(t, testCreator, 0, from.CategoryId)
	sentinel := types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL

	_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
		Creator: testSentinel, PostId: own.PostId, Authority: sentinel,
		ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
	})
	require.ErrorIs(t, err, types.ErrSelfModeration)

	_, err = f.msgServer.LockThread(f.ctx, &types.MsgLockThread{
		Creator: testSentinel, RootId: own.PostId, Reason: "closing my own thread", Authority: sentinel,
	})
	require.ErrorIs(t, err, types.ErrSelfModeration)

	_, err = f.msgServer.MoveThread(f.ctx, &types.MsgMoveThread{
		Creator: testSentinel, RootId: own.PostId, NewCategoryId: to.CategoryId, Reason: "elsewhere", Authority: sentinel,
	})
	require.ErrorIs(t, err, types.ErrSelfModeration)

	// Someone else's content is still fair game.
	_, err = f.msgServer.LockThread(f.ctx, &types.MsgLockThread{
		Creator: testSentinel, RootId: other.PostId, Reason: "off-topic", Authority: sentinel,
	})
	require.NoError(t, err)
}

// FRM-30: MsgDismissFlags honours both pause switches, for sentinels and
// for the council.
func TestDismissFlagsHonoursPauses(t *testing.T) {
	f := initFixture(t)
	f.createTestSentinel(t, testSentinel, "5000000000")
	authority, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	require.NoError(t, err)
	for _, pause := range []struct {
		name string
		set  func(*types.Params)
		want error
	}{
		{"forum paused", func(p *types.Params) { p.ForumPaused = true }, types.ErrForumPaused},
		{"moderation paused", func(p *types.Params) { p.ModerationPaused = true }, types.ErrModerationPaused},
	} {
		params := types.DefaultParams()
		pause.set(&params)
		require.NoError(t, f.keeper.Params.Set(f.ctx, params))
		for _, caller := range []string{testSentinel, authority} {
			_, err := f.msgServer.DismissFlags(f.ctx, &types.MsgDismissFlags{Creator: caller, PostId: 1})
			require.ErrorIs(t, err, pause.want, "%s / %s", pause.name, caller)
		}
	}
}

// FRM-31: moving a pinned thread moves its pinned-index entry, so it lists
// under the new category only and unpinning leaves no stale entry.
func TestMovingPinnedThreadReKeysPinIndex(t *testing.T) {
	f := initFixture(t)
	from := f.createTestCategory(t, "General")
	to := f.createTestCategory(t, "Elsewhere")
	thread := f.createTestPost(t, testCreator, 0, from.CategoryId)
	thread.Pinned = true
	require.NoError(t, f.keeper.Post.Set(f.ctx, thread.PostId, thread))
	require.NoError(t, f.keeper.PostsByPinned.Set(f.ctx, collections.Join(from.CategoryId, thread.PostId)))

	authority, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	require.NoError(t, err)
	_, err = f.msgServer.MoveThread(f.ctx, &types.MsgMoveThread{
		Creator: authority, RootId: thread.PostId, NewCategoryId: to.CategoryId, Reason: "wrong board",
		Authority: types.ModerationAuthority_MODERATION_AUTHORITY_COUNCIL,
	})
	require.NoError(t, err)

	has, err := f.keeper.PostsByPinned.Has(f.ctx, collections.Join(from.CategoryId, thread.PostId))
	require.NoError(t, err)
	require.False(t, has, "no stale entry under the old category")
	has, err = f.keeper.PostsByPinned.Has(f.ctx, collections.Join(to.CategoryId, thread.PostId))
	require.NoError(t, err)
	require.True(t, has, "listed under the new category")
}

// FRM-33: category deletion walks the category's thread roots; a moved
// thread counts for its new category only, and its replies' creation-time
// category no longer blocks deleting the one it left.
func TestHasPostInCategoryFollowsMovedRoot(t *testing.T) {
	f := initFixture(t)
	from := f.createTestCategory(t, "General")
	to := f.createTestCategory(t, "Elsewhere")
	thread := f.createTestPost(t, testCreator, 0, from.CategoryId)
	f.createTestPost(t, testCreator2, thread.PostId, from.CategoryId)

	authority, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	require.NoError(t, err)
	_, err = f.msgServer.MoveThread(f.ctx, &types.MsgMoveThread{
		Creator: authority, RootId: thread.PostId, NewCategoryId: to.CategoryId, Reason: "wrong board",
		Authority: types.ModerationAuthority_MODERATION_AUTHORITY_COUNCIL,
	})
	require.NoError(t, err)

	has, err := f.keeper.HasPostInCategory(f.ctx, from.CategoryId)
	require.NoError(t, err)
	require.False(t, has, "the old category is empty once its only thread moved")
	has, err = f.keeper.HasPostInCategory(f.ctx, to.CategoryId)
	require.NoError(t, err)
	require.True(t, has)
}
