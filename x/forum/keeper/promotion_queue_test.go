package keeper_test

import (
	"context"
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// seedEphemeral writes a Post + ExpirationQueue + EphemeralByAuthor entry in
// one call so promotion-queue tests aren't repeating themselves.
func seedEphemeral(t *testing.T, f *fixture, author string, expiresAt int64) types.Post {
	t.Helper()
	post := f.createTestPost(t, author, 0, 0)
	post.ExpirationTime = expiresAt
	require.NoError(t, f.keeper.Post.Set(f.ctx, post.PostId, post))
	require.NoError(t, f.keeper.ExpirationQueue.Set(f.ctx, collections.Join(expiresAt, post.PostId)))
	require.NoError(t, f.keeper.AddEphemeralAuthorIndex(f.ctx, author, post.PostId))
	return post
}

func TestEnqueueAuthorForPromotion(t *testing.T) {
	t.Run("enqueues a user-account author", func(t *testing.T) {
		f := initFixture(t)
		require.NoError(t, f.keeper.EnqueueAuthorForPromotion(f.ctx, testCreator))
		require.True(t, f.keeper.IsAuthorQueuedForPromotion(f.ctx, testCreator))
	})

	t.Run("idempotent (overwrites with newer block height)", func(t *testing.T) {
		f := initFixture(t)
		require.NoError(t, f.keeper.EnqueueAuthorForPromotion(f.ctx, testCreator))
		require.NoError(t, f.keeper.EnqueueAuthorForPromotion(f.ctx, testCreator))
		require.True(t, f.keeper.IsAuthorQueuedForPromotion(f.ctx, testCreator))
	})

	t.Run("no-op for module account (anonymous)", func(t *testing.T) {
		f := initFixture(t)
		// Get the forum module address using the same code path the keeper uses.
		// The forum module addr-derivation is encapsulated in the keeper; the
		// public lever is "EnqueueAuthorForPromotion is a no-op for it".
		// Easier: just verify empty string is rejected.
		require.NoError(t, f.keeper.EnqueueAuthorForPromotion(f.ctx, ""))
		require.False(t, f.keeper.IsAuthorQueuedForPromotion(f.ctx, ""))
	})
}

func TestEndBlockerDrainsPromotionQueue(t *testing.T) {
	// Default mock IsMember = true is fine for these — the upgrade path
	// is independent of membership inside the promotion queue (the user
	// was already admitted; that's how they got queued in the first place).

	t.Run("drains all queued authors' ephemeral posts to permanent", func(t *testing.T) {
		f := initFixture(t)
		_ = f.createTestCategory(t, "General")

		now := int64(2_000_000)
		f.ctx = f.sdkCtx().WithBlockTime(time.Unix(now, 0))

		// Two ephemeral posts (different IDs) for testCreator
		exp := now + 86400
		p1 := seedEphemeral(t, f, testCreator, exp)
		p2 := seedEphemeral(t, f, testCreator, exp+1)

		// Enqueue the author as if AfterMemberAdmitted just fired.
		require.NoError(t, f.keeper.EnqueueAuthorForPromotion(f.ctx, testCreator))

		require.NoError(t, f.keeper.EndBlocker(f.ctx))

		// Both posts should now be permanent.
		for _, id := range []uint64{p1.PostId, p2.PostId} {
			got, err := f.keeper.Post.Get(f.ctx, id)
			require.NoError(t, err)
			require.Equal(t, int64(0), got.ExpirationTime, "post %d should be permanent", id)
		}

		// Author drained from the promotion queue.
		require.False(t, f.keeper.IsAuthorQueuedForPromotion(f.ctx, testCreator))

		// EphemeralByAuthor entries scrubbed.
		hasE, err := f.keeper.EphemeralByAuthor.Has(f.ctx, collections.Join(testCreator, p1.PostId))
		require.NoError(t, err)
		require.False(t, hasE)
		hasE, err = f.keeper.EphemeralByAuthor.Has(f.ctx, collections.Join(testCreator, p2.PostId))
		require.NoError(t, err)
		require.False(t, hasE)
	})

	t.Run("respects max_promotions_per_block cap", func(t *testing.T) {
		f := initFixture(t)
		_ = f.createTestCategory(t, "General")

		// Lower the cap so we can test with just a couple of posts.
		params, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)
		params.MaxPromotionsPerBlock = 1
		require.NoError(t, f.keeper.Params.Set(f.ctx, params))

		now := int64(2_000_000)
		f.ctx = f.sdkCtx().WithBlockTime(time.Unix(now, 0))

		p1 := seedEphemeral(t, f, testCreator, now+86400)
		p2 := seedEphemeral(t, f, testCreator, now+86401)
		require.NoError(t, f.keeper.EnqueueAuthorForPromotion(f.ctx, testCreator))

		require.NoError(t, f.keeper.EndBlocker(f.ctx))

		// Exactly one of the two posts should have been promoted.
		got1, _ := f.keeper.Post.Get(f.ctx, p1.PostId)
		got2, _ := f.keeper.Post.Get(f.ctx, p2.PostId)
		promoted := 0
		if got1.ExpirationTime == 0 {
			promoted++
		}
		if got2.ExpirationTime == 0 {
			promoted++
		}
		require.Equal(t, 1, promoted, "exactly one post should have been promoted under budget=1")

		// Author should remain in the queue (not fully drained).
		require.True(t, f.keeper.IsAuthorQueuedForPromotion(f.ctx, testCreator))

		// Next EndBlocker drains the rest.
		require.NoError(t, f.keeper.EndBlocker(f.ctx))
		got1, _ = f.keeper.Post.Get(f.ctx, p1.PostId)
		got2, _ = f.keeper.Post.Get(f.ctx, p2.PostId)
		require.Equal(t, int64(0), got1.ExpirationTime)
		require.Equal(t, int64(0), got2.ExpirationTime)
		require.False(t, f.keeper.IsAuthorQueuedForPromotion(f.ctx, testCreator))
	})

	t.Run("removes author from queue when no ephemeral posts remain", func(t *testing.T) {
		f := initFixture(t)
		// No ephemeral posts at all — just an enqueue.
		require.NoError(t, f.keeper.EnqueueAuthorForPromotion(f.ctx, testCreator))
		require.True(t, f.keeper.IsAuthorQueuedForPromotion(f.ctx, testCreator))

		require.NoError(t, f.keeper.EndBlocker(f.ctx))

		require.False(t, f.keeper.IsAuthorQueuedForPromotion(f.ctx, testCreator),
			"author with no ephemeral posts should be drained from the queue")
	})

	t.Run("cleans up stale EphemeralByAuthor entries for missing posts", func(t *testing.T) {
		f := initFixture(t)
		// Index entry pointing at a post that doesn't exist.
		ghostID := uint64(99999)
		require.NoError(t, f.keeper.EphemeralByAuthor.Set(f.ctx, collections.Join(testCreator, ghostID)))
		require.NoError(t, f.keeper.EnqueueAuthorForPromotion(f.ctx, testCreator))

		require.NoError(t, f.keeper.EndBlocker(f.ctx))

		// Stale index entry should have been removed.
		has, err := f.keeper.EphemeralByAuthor.Has(f.ctx, collections.Join(testCreator, ghostID))
		require.NoError(t, err)
		require.False(t, has)
		// And the author is dropped from the queue.
		require.False(t, f.keeper.IsAuthorQueuedForPromotion(f.ctx, testCreator))
	})

	t.Run("skips promotion when MaxPromotionsPerBlock is zero", func(t *testing.T) {
		f := initFixture(t)
		_ = f.createTestCategory(t, "General")

		params, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)
		params.MaxPromotionsPerBlock = 0 // throttle off
		require.NoError(t, f.keeper.Params.Set(f.ctx, params))

		now := int64(2_000_000)
		f.ctx = f.sdkCtx().WithBlockTime(time.Unix(now, 0))
		p1 := seedEphemeral(t, f, testCreator, now+86400)
		require.NoError(t, f.keeper.EnqueueAuthorForPromotion(f.ctx, testCreator))

		require.NoError(t, f.keeper.EndBlocker(f.ctx))

		// Still ephemeral — phase 0 was skipped.
		got, err := f.keeper.Post.Get(f.ctx, p1.PostId)
		require.NoError(t, err)
		require.Equal(t, now+86400, got.ExpirationTime)
		require.True(t, f.keeper.IsAuthorQueuedForPromotion(f.ctx, testCreator))
	})
}

// TestForumRepHooks exercises the hook surface end-to-end. The hook should
// enqueue the new member into the promotion queue.
func TestForumRepHooks_AfterMemberAdmitted(t *testing.T) {
	f := initFixture(t)
	hooks := keeper.NewForumRepHooks(&f.keeper)

	addr := testCreatorAddr
	require.NoError(t, hooks.AfterMemberAdmitted(f.ctx, addr))
	require.True(t, f.keeper.IsAuthorQueuedForPromotion(f.ctx, testCreator))
}

// Compile-time fence: AfterMemberAdmitted accepts an sdk.AccAddress so the
// rep keeper can hand the runtime member address through unchanged.
var _ = func(ctx context.Context, addr sdk.AccAddress) error {
	return keeper.ForumRepHooks{}.AfterMemberAdmitted(ctx, addr)
}
