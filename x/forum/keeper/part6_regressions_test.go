package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

// FRM-102: an ephemeral post hidden shortly before its TTL keeps the full
// appeal window: the TTL prune defers to the hide's own expiry instead of
// settling the hide before an appeal could open.
func TestEphemeralHideNearTTLKeepsAppealWindow(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "eph-window")
	f.createTestSentinel(t, testSentinel, "2000000000")
	f.repKeeper.IsMemberFn = func(context.Context, sdk.AccAddress) bool { return false }

	ttl := reviewNow + 5 // the TTL lands before the hide's appeal window closes
	p := f.createTestPost(t, testCreator2, 0, cat.CategoryId)
	p.ExpirationTime = ttl
	require.NoError(t, f.keeper.Post.Set(f.ctx, p.PostId, p))
	require.NoError(t, f.keeper.ExpirationQueue.Set(f.ctx, collections.Join(ttl, p.PostId)))
	f.sentinelHide(t, testSentinel, p.PostId)
	hr, err := f.keeper.HideRecord.Get(f.ctx, p.PostId)
	require.NoError(t, err)
	hideExpiry := hr.HiddenAt + types.DefaultHiddenExpiration
	require.Greater(t, hideExpiry, ttl)

	f.at(ttl)
	require.NoError(t, f.keeper.PruneExpiredPosts(f.ctx, ttl))
	got, err := f.keeper.Post.Get(f.ctx, p.PostId)
	require.NoError(t, err, "the hidden post survives its TTL while the hide is appealable")
	require.Equal(t, types.PostStatus_POST_STATUS_HIDDEN, got.Status)
	require.Equal(t, hideExpiry+1, got.ExpirationTime)
	_, err = f.keeper.HideRecord.Get(f.ctx, p.PostId)
	require.NoError(t, err, "the hide is still open")

	// The author can appeal once the cooldown passes.
	f.at(hr.HiddenAt + types.DefaultHideAppealCooldown + 1)
	_, err = f.msgServer.AppealPost(f.ctx, &types.MsgAppealPost{Creator: testCreator2, PostId: p.PostId})
	require.NoError(t, err)
}

// FRM-102: a hide on an anonymous post can never be appealed, so when it
// stands the stakers are not slashed and the sentinel earns no unchallenged
// credit.
func TestUnappealableAnonymousHideDoesNotSlashStakers(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "anon-hide")
	f.createTestSentinel(t, testSentinel, "2000000000")

	shieldAddr := authtypes.NewModuleAddress("shield").String()
	p := f.createTestPost(t, testCreator2, 0, cat.CategoryId)
	p.Author = shieldAddr
	require.NoError(t, f.keeper.Post.Set(f.ctx, p.PostId, p))

	stake := types.PostConvictionStake{Id: 1, Staker: testCreator, PostId: p.PostId, Amount: math.NewInt(1_000_000), StakedAt: reviewNow}
	require.NoError(t, f.keeper.PostConvictionStake.Set(f.ctx, stake.Id, stake))
	require.NoError(t, f.keeper.PostConvictionStakesByPost.Set(f.ctx, collections.Join(p.PostId, stake.Id)))
	require.NoError(t, f.keeper.PostConvictionStakesByStaker.Set(f.ctx, collections.Join(stake.Staker, stake.Id)))

	f.sentinelHide(t, testSentinel, p.PostId)
	hr, err := f.keeper.HideRecord.Get(f.ctx, p.PostId)
	require.NoError(t, err)
	end := hr.HiddenAt + types.DefaultHiddenExpiration
	f.at(end)
	require.NoError(t, f.keeper.ExpireHiddenPosts(f.ctx, end))

	got, err := f.keeper.PostConvictionStake.Get(f.ctx, stake.Id)
	require.NoError(t, err)
	require.True(t, got.Released, "the stake is closed")
	require.Empty(t, f.repKeeper.burnDreamCalls, "no staker slash")
	act, err := f.keeper.SentinelActivity.Get(f.ctx, testSentinel)
	require.NoError(t, err)
	require.Zero(t, act.UnchallengedHides, "no unchallenged credit for an unappealable hide")
}

// FRM-104: forum_paused stops the sentinel and council moderation actions
// the spec gates (hide, pin, unpin, unlock) and unfollow; appeals stay open.
func TestForumPausedGatesModerationActions(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "paused")
	f.createTestSentinel(t, testSentinel, "2000000000")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	hidden := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	f.sentinelHide(t, testSentinel, hidden.PostId)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.ForumPaused = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	gov := f.govAuthority(t)

	_, err = f.msgServer.HidePost(f.ctx, &types.MsgHidePost{Creator: testSentinel, PostId: post.PostId, ReasonCode: 1, Authority: types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL})
	require.ErrorIs(t, err, types.ErrForumPaused)
	_, err = f.msgServer.PinPost(f.ctx, &types.MsgPinPost{Creator: gov, PostId: post.PostId})
	require.ErrorIs(t, err, types.ErrForumPaused)
	_, err = f.msgServer.UnpinPost(f.ctx, &types.MsgUnpinPost{Creator: gov, PostId: post.PostId})
	require.ErrorIs(t, err, types.ErrForumPaused)
	_, err = f.msgServer.UnlockThread(f.ctx, &types.MsgUnlockThread{Creator: gov, RootId: post.PostId})
	require.ErrorIs(t, err, types.ErrForumPaused)
	_, err = f.msgServer.UnfollowThread(f.ctx, &types.MsgUnfollowThread{Creator: testCreator2, ThreadId: post.PostId})
	require.ErrorIs(t, err, types.ErrForumPaused)

	// Appeals stay open: a blocked window would let the hide finalize.
	f.at(reviewNow + types.DefaultHideAppealCooldown + 1)
	_, err = f.msgServer.AppealPost(f.ctx, &types.MsgAppealPost{Creator: testCreator, PostId: hidden.PostId})
	require.NoError(t, err)
}

// FRM-107: a deleted root leaves RootsByCategory, so HasPostInCategory never
// walks it again.
func TestDeletedRootLeavesCategoryIndex(t *testing.T) {
	f := initFixture(t)
	f.at(reviewNow)
	cat := f.createTestCategory(t, "roots")
	root := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	_, err := f.msgServer.DeletePost(f.ctx, &types.MsgDeletePost{Creator: testCreator, PostId: root.PostId})
	require.NoError(t, err)
	has, err := f.keeper.RootsByCategory.Has(f.ctx, collections.Join(cat.CategoryId, root.PostId))
	require.NoError(t, err)
	require.False(t, has)
	live, err := f.keeper.HasPostInCategory(f.ctx, cat.CategoryId)
	require.NoError(t, err)
	require.False(t, live)
}

// FRM-107: the promotion drain reads no more than its budget per block and
// still drains every queued author over later blocks.
func TestPromotionDrainIsBudgetBounded(t *testing.T) {
	f := initFixture(t)
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxPromotionsPerBlock = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	f.at(reviewNow)

	a1 := seedEphemeral(t, f, testCreator, reviewNow+86400)
	a2 := seedEphemeral(t, f, testCreator, reviewNow+86401)
	b1 := seedEphemeral(t, f, testCreator2, reviewNow+86402)
	require.NoError(t, f.keeper.EnqueueAuthorForPromotion(f.ctx, testCreator))
	require.NoError(t, f.keeper.EnqueueAuthorForPromotion(f.ctx, testCreator2))

	permanent := func() int {
		n := 0
		for _, id := range []uint64{a1.PostId, a2.PostId, b1.PostId} {
			p, err := f.keeper.Post.Get(f.ctx, id)
			require.NoError(t, err)
			if p.ExpirationTime == 0 {
				n++
			}
		}
		return n
	}
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	require.Equal(t, 1, permanent())
	for i := 0; i < 4; i++ {
		require.NoError(t, f.keeper.EndBlocker(f.ctx))
	}
	require.Equal(t, 3, permanent())
	require.False(t, f.keeper.IsAuthorQueuedForPromotion(f.ctx, testCreator))
	require.False(t, f.keeper.IsAuthorQueuedForPromotion(f.ctx, testCreator2))
}
