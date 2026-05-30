package keeper_test

import (
	"context"
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	reptypes "sparkdream/x/rep/types"

	"sparkdream/x/forum/types"
)

// markNonMember forces the mock RepKeeper's IsMember to return false for every
// address. Hard-delete tests need this because the default mock reports the
// author as a member, which now triggers the upgrade-if-member path in the
// EndBlocker (hard-delete is reserved for non-members under the new flow).
func markNonMember(f *fixture) {
	f.repKeeper.IsMemberFn = func(_ context.Context, _ sdk.AccAddress) bool { return false }
}

func TestPruneExpiredPosts(t *testing.T) {
	t.Run("prunes expired ephemeral posts", func(t *testing.T) {
		f := initFixture(t)
		markNonMember(f)
		cat := f.createTestCategory(t, "General")

		now := int64(1000000)
		ctx := f.sdkCtx().WithBlockTime(time.Unix(now, 0))
		f.ctx = ctx

		// Create an ephemeral post that expired at now-100
		postID, err := f.keeper.PostSeq.Next(f.ctx)
		require.NoError(t, err)

		expirationTime := now - 100
		post := types.Post{
			PostId:         postID,
			CategoryId:     cat.CategoryId,
			Author:         testCreator,
			Content:        "Ephemeral post",
			CreatedAt:      now - 200,
			ExpirationTime: expirationTime,
			Status:         types.PostStatus_POST_STATUS_ACTIVE,
		}
		require.NoError(t, f.keeper.Post.Set(f.ctx, postID, post))
		require.NoError(t, f.keeper.ExpirationQueue.Set(f.ctx, collections.Join(expirationTime, postID)))

		// Verify post exists before pruning
		_, err = f.keeper.Post.Get(f.ctx, postID)
		require.NoError(t, err)

		// Run EndBlocker
		err = f.keeper.EndBlocker(f.ctx)
		require.NoError(t, err)

		// Post should be deleted
		_, err = f.keeper.Post.Get(f.ctx, postID)
		require.Error(t, err)

		// Queue entry should be removed
		has, err := f.keeper.ExpirationQueue.Has(f.ctx, collections.Join(expirationTime, postID))
		require.NoError(t, err)
		require.False(t, has)
	})

	t.Run("does not prune non-expired posts", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "General")

		now := int64(1000000)
		ctx := f.sdkCtx().WithBlockTime(time.Unix(now, 0))
		f.ctx = ctx

		// Create an ephemeral post that expires in the future
		postID, err := f.keeper.PostSeq.Next(f.ctx)
		require.NoError(t, err)

		futureExpiration := now + 86400
		post := types.Post{
			PostId:         postID,
			CategoryId:     cat.CategoryId,
			Author:         testCreator,
			Content:        "Future ephemeral post",
			CreatedAt:      now,
			ExpirationTime: futureExpiration,
			Status:         types.PostStatus_POST_STATUS_ACTIVE,
		}
		require.NoError(t, f.keeper.Post.Set(f.ctx, postID, post))
		require.NoError(t, f.keeper.ExpirationQueue.Set(f.ctx, collections.Join(futureExpiration, postID)))

		// Run EndBlocker
		err = f.keeper.EndBlocker(f.ctx)
		require.NoError(t, err)

		// Post should still exist
		_, err = f.keeper.Post.Get(f.ctx, postID)
		require.NoError(t, err)

		// Queue entry should still exist
		has, err := f.keeper.ExpirationQueue.Has(f.ctx, collections.Join(futureExpiration, postID))
		require.NoError(t, err)
		require.True(t, has)
	})

	t.Run("cleans stale queue entries for salvaged posts", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "General")

		now := int64(1000000)
		ctx := f.sdkCtx().WithBlockTime(time.Unix(now, 0))
		f.ctx = ctx

		// Create a post that was salvaged (ExpirationTime=0) but queue entry remains
		postID, err := f.keeper.PostSeq.Next(f.ctx)
		require.NoError(t, err)

		originalExpiration := now - 50
		post := types.Post{
			PostId:         postID,
			CategoryId:     cat.CategoryId,
			Author:         testCreator,
			Content:        "Salvaged post",
			CreatedAt:      now - 200,
			ExpirationTime: 0, // salvaged - made permanent
			Status:         types.PostStatus_POST_STATUS_ACTIVE,
		}
		require.NoError(t, f.keeper.Post.Set(f.ctx, postID, post))
		// Stale queue entry from before salvation
		require.NoError(t, f.keeper.ExpirationQueue.Set(f.ctx, collections.Join(originalExpiration, postID)))

		// Run EndBlocker
		err = f.keeper.EndBlocker(f.ctx)
		require.NoError(t, err)

		// Post should still exist (was salvaged, not pruned)
		savedPost, err := f.keeper.Post.Get(f.ctx, postID)
		require.NoError(t, err)
		require.Equal(t, int64(0), savedPost.ExpirationTime)

		// Stale queue entry should be removed
		has, err := f.keeper.ExpirationQueue.Has(f.ctx, collections.Join(originalExpiration, postID))
		require.NoError(t, err)
		require.False(t, has)
	})

	t.Run("cleans stale queue entries for deleted posts", func(t *testing.T) {
		f := initFixture(t)

		now := int64(1000000)
		ctx := f.sdkCtx().WithBlockTime(time.Unix(now, 0))
		f.ctx = ctx

		// Queue entry references a post that no longer exists
		ghostPostID := uint64(99999)
		ghostExpiration := now - 50
		require.NoError(t, f.keeper.ExpirationQueue.Set(f.ctx, collections.Join(ghostExpiration, ghostPostID)))

		// Run EndBlocker
		err := f.keeper.EndBlocker(f.ctx)
		require.NoError(t, err)

		// Stale queue entry should be removed
		has, err := f.keeper.ExpirationQueue.Has(f.ctx, collections.Join(ghostExpiration, ghostPostID))
		require.NoError(t, err)
		require.False(t, has)
	})

	t.Run("cleans up PostFlag and HideRecord on prune", func(t *testing.T) {
		f := initFixture(t)
		markNonMember(f)
		cat := f.createTestCategory(t, "General")

		now := int64(1000000)
		ctx := f.sdkCtx().WithBlockTime(time.Unix(now, 0))
		f.ctx = ctx

		postID, err := f.keeper.PostSeq.Next(f.ctx)
		require.NoError(t, err)

		expirationTime := now - 100
		post := types.Post{
			PostId:         postID,
			CategoryId:     cat.CategoryId,
			Author:         testCreator,
			Content:        "Flagged ephemeral post",
			CreatedAt:      now - 200,
			ExpirationTime: expirationTime,
			Status:         types.PostStatus_POST_STATUS_ACTIVE,
		}
		require.NoError(t, f.keeper.Post.Set(f.ctx, postID, post))
		require.NoError(t, f.keeper.ExpirationQueue.Set(f.ctx, collections.Join(expirationTime, postID)))

		// Add PostFlag and HideRecord
		require.NoError(t, f.keeper.PostFlag.Set(f.ctx, postID, types.PostFlag{
			PostId: postID,
		}))
		require.NoError(t, f.keeper.HideRecord.Set(f.ctx, postID, types.HideRecord{
			PostId: postID,
		}))

		// Run EndBlocker
		err = f.keeper.EndBlocker(f.ctx)
		require.NoError(t, err)

		// Post, PostFlag, and HideRecord should all be removed
		_, err = f.keeper.Post.Get(f.ctx, postID)
		require.Error(t, err)

		has, err := f.keeper.PostFlag.Has(f.ctx, postID)
		require.NoError(t, err)
		require.False(t, has)

		has, err = f.keeper.HideRecord.Has(f.ctx, postID)
		require.NoError(t, err)
		require.False(t, has)
	})

	t.Run("respects maxPrunePerBlock limit", func(t *testing.T) {
		f := initFixture(t)
		markNonMember(f)
		cat := f.createTestCategory(t, "General")

		now := int64(1000000)
		ctx := f.sdkCtx().WithBlockTime(time.Unix(now, 0))
		f.ctx = ctx

		// Create 105 expired ephemeral posts (limit is 100)
		totalPosts := 105
		for i := 0; i < totalPosts; i++ {
			postID, err := f.keeper.PostSeq.Next(f.ctx)
			require.NoError(t, err)

			expirationTime := now - int64(totalPosts-i) // all expired
			post := types.Post{
				PostId:         postID,
				CategoryId:     cat.CategoryId,
				Author:         testCreator,
				Content:        "Bulk ephemeral post",
				CreatedAt:      now - 200,
				ExpirationTime: expirationTime,
				Status:         types.PostStatus_POST_STATUS_ACTIVE,
			}
			require.NoError(t, f.keeper.Post.Set(f.ctx, postID, post))
			require.NoError(t, f.keeper.ExpirationQueue.Set(f.ctx, collections.Join(expirationTime, postID)))
		}

		// Run EndBlocker once
		err := f.keeper.EndBlocker(f.ctx)
		require.NoError(t, err)

		// Count remaining queue entries
		remaining := 0
		err = f.keeper.ExpirationQueue.Walk(f.ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
			remaining++
			return false, nil
		})
		require.NoError(t, err)

		// Should have 5 remaining (105 - 100 pruned)
		require.Equal(t, 5, remaining)

		// Run EndBlocker again to prune the rest
		err = f.keeper.EndBlocker(f.ctx)
		require.NoError(t, err)

		remaining = 0
		err = f.keeper.ExpirationQueue.Walk(f.ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
			remaining++
			return false, nil
		})
		require.NoError(t, err)
		require.Equal(t, 0, remaining)
	})

	t.Run("prunes posts exactly at block time", func(t *testing.T) {
		f := initFixture(t)
		markNonMember(f)
		cat := f.createTestCategory(t, "General")

		now := int64(1000000)
		ctx := f.sdkCtx().WithBlockTime(time.Unix(now, 0))
		f.ctx = ctx

		// Create a post expiring exactly at now
		postID, err := f.keeper.PostSeq.Next(f.ctx)
		require.NoError(t, err)

		post := types.Post{
			PostId:         postID,
			CategoryId:     cat.CategoryId,
			Author:         testCreator,
			Content:        "Exact expiry post",
			CreatedAt:      now - 100,
			ExpirationTime: now, // expires exactly at block time
			Status:         types.PostStatus_POST_STATUS_ACTIVE,
		}
		require.NoError(t, f.keeper.Post.Set(f.ctx, postID, post))
		require.NoError(t, f.keeper.ExpirationQueue.Set(f.ctx, collections.Join(now, postID)))

		// Run EndBlocker
		err = f.keeper.EndBlocker(f.ctx)
		require.NoError(t, err)

		// Post should be deleted (EndInclusive includes exact match)
		_, err = f.keeper.Post.Get(f.ctx, postID)
		require.Error(t, err)
	})

	t.Run("emits ephemeral_post_pruned events", func(t *testing.T) {
		f := initFixture(t)
		markNonMember(f)
		cat := f.createTestCategory(t, "General")

		now := int64(1000000)
		sdkCtx := f.sdkCtx().WithBlockTime(time.Unix(now, 0))
		f.ctx = sdkCtx

		postID, err := f.keeper.PostSeq.Next(f.ctx)
		require.NoError(t, err)

		expirationTime := now - 100
		post := types.Post{
			PostId:         postID,
			CategoryId:     cat.CategoryId,
			Author:         testCreator,
			Content:        "Event test post",
			CreatedAt:      now - 200,
			ExpirationTime: expirationTime,
			Status:         types.PostStatus_POST_STATUS_ACTIVE,
		}
		require.NoError(t, f.keeper.Post.Set(f.ctx, postID, post))
		require.NoError(t, f.keeper.ExpirationQueue.Set(f.ctx, collections.Join(expirationTime, postID)))

		// Run EndBlocker
		err = f.keeper.EndBlocker(f.ctx)
		require.NoError(t, err)

		// Check for ephemeral_post_pruned event
		events := sdk.UnwrapSDKContext(f.ctx).EventManager().Events()
		found := false
		for _, event := range events {
			if event.Type == "ephemeral_post_pruned" {
				found = true
				break
			}
		}
		require.True(t, found, "expected ephemeral_post_pruned event")
	})

	t.Run("no-op when queue is empty", func(t *testing.T) {
		f := initFixture(t)

		now := int64(1000000)
		ctx := f.sdkCtx().WithBlockTime(time.Unix(now, 0))
		f.ctx = ctx

		// Run EndBlocker with empty queue
		err := f.keeper.EndBlocker(f.ctx)
		require.NoError(t, err)
	})

	// Regression: TTL prune of an ephemeral post must decrement UsageCount
	// for every tag the post carried. Cousin of TestDeletePostDecrementsTagUsage
	// for the EndBlocker path — without this, ephemeral-post churn inflates
	// UsageCount monotonically and ExpireTags loses its grip on idle tags.
	t.Run("decrements tag usage on tombstone", func(t *testing.T) {
		f := initFixture(t)
		markNonMember(f)
		cat := f.createTestCategory(t, "General")

		now := int64(1000000)
		f.ctx = f.sdkCtx().WithBlockTime(time.Unix(now, 0))

		// Seed two tags with UsageCount=1 to model the post being a live reference.
		f.repKeeper.tags = map[string]reptypes.Tag{
			"alpha": {Name: "alpha", UsageCount: 1},
			"beta":  {Name: "beta", UsageCount: 1},
		}

		postID, err := f.keeper.PostSeq.Next(f.ctx)
		require.NoError(t, err)

		expirationTime := now - 100
		post := types.Post{
			PostId:         postID,
			CategoryId:     cat.CategoryId,
			Author:         testCreator,
			Content:        "Tagged ephemeral",
			CreatedAt:      now - 200,
			ExpirationTime: expirationTime,
			Status:         types.PostStatus_POST_STATUS_ACTIVE,
			Tags:           []string{"alpha", "beta"},
		}
		require.NoError(t, f.keeper.Post.Set(f.ctx, postID, post))
		require.NoError(t, f.keeper.ExpirationQueue.Set(f.ctx, collections.Join(expirationTime, postID)))

		require.NoError(t, f.keeper.EndBlocker(f.ctx))

		// Post hard-deleted.
		_, err = f.keeper.Post.Get(f.ctx, postID)
		require.Error(t, err)

		// Each tag decremented exactly once (1 -> 0).
		require.Equal(t, uint64(0), f.repKeeper.tags["alpha"].UsageCount,
			"tag alpha usage must drop on TTL prune")
		require.Equal(t, uint64(0), f.repKeeper.tags["beta"].UsageCount,
			"tag beta usage must drop on TTL prune")
	})

	// Upgrade-if-now-member lazy fallback: an expired ephemeral post whose
	// author is now an active member should flip to permanent rather than
	// hard-delete. This catches authors who joined after the promotion queue
	// was already drained, or posts created before EphemeralByAuthor existed.
	t.Run("upgrades to permanent when author is now a member", func(t *testing.T) {
		f := initFixture(t)
		// Default mock IsMember=true is what we want here.
		cat := f.createTestCategory(t, "General")

		now := int64(1000000)
		f.ctx = f.sdkCtx().WithBlockTime(time.Unix(now, 0))

		postID, err := f.keeper.PostSeq.Next(f.ctx)
		require.NoError(t, err)

		expirationTime := now - 100
		post := types.Post{
			PostId:         postID,
			CategoryId:     cat.CategoryId,
			Author:         testCreator,
			Content:        "Ephemeral while not a member",
			CreatedAt:      now - 200,
			ExpirationTime: expirationTime,
			Status:         types.PostStatus_POST_STATUS_ACTIVE,
		}
		require.NoError(t, f.keeper.Post.Set(f.ctx, postID, post))
		require.NoError(t, f.keeper.ExpirationQueue.Set(f.ctx, collections.Join(expirationTime, postID)))

		require.NoError(t, f.keeper.EndBlocker(f.ctx))

		got, err := f.keeper.Post.Get(f.ctx, postID)
		require.NoError(t, err, "post must NOT be hard-deleted when author is now a member")
		require.Equal(t, int64(0), got.ExpirationTime, "ExpirationTime must be cleared (permanent)")
		require.Equal(t, types.PostStatus_POST_STATUS_ACTIVE, got.Status)

		// Queue entry purged.
		has, err := f.keeper.ExpirationQueue.Has(f.ctx, collections.Join(expirationTime, postID))
		require.NoError(t, err)
		require.False(t, has)
	})
}

// ExpireHiddenPosts is the second forum tombstone path that must decrement
// tag usage. A post hidden by a sentinel that goes unappealed for
// DefaultHiddenExpiration (7d) gets soft-deleted by the EndBlocker; without
// the decrement its tags' UsageCount would stay inflated forever.
func TestExpireHiddenPosts_DecrementsTagUsage(t *testing.T) {
	f := initFixture(t)
	cat := f.createTestCategory(t, "General")

	hiddenAt := int64(1_000_000)
	now := hiddenAt + types.DefaultHiddenExpiration + 1 // past expiry
	f.ctx = f.sdkCtx().WithBlockTime(time.Unix(now, 0))

	// Seed one tag with UsageCount=1.
	f.repKeeper.tags = map[string]reptypes.Tag{
		"gamma": {Name: "gamma", UsageCount: 1},
	}

	postID, err := f.keeper.PostSeq.Next(f.ctx)
	require.NoError(t, err)

	post := types.Post{
		PostId:     postID,
		CategoryId: cat.CategoryId,
		Author:     testCreator,
		Content:    "Hidden post",
		CreatedAt:  hiddenAt - 100,
		Status:     types.PostStatus_POST_STATUS_HIDDEN,
		HiddenAt:   hiddenAt,
		Tags:       []string{"gamma"},
	}
	require.NoError(t, f.keeper.Post.Set(f.ctx, postID, post))
	require.NoError(t, f.keeper.HideRecord.Set(f.ctx, postID, types.HideRecord{
		PostId:   postID,
		HiddenAt: hiddenAt,
	}))

	require.NoError(t, f.keeper.ExpireHiddenPosts(f.ctx, now))

	// Post soft-deleted; tag list severed; HideRecord gone.
	got, err := f.keeper.Post.Get(f.ctx, postID)
	require.NoError(t, err)
	require.Equal(t, types.PostStatus_POST_STATUS_DELETED, got.Status)
	require.Nil(t, got.Tags, "hidden-expire path must clear post.Tags")
	require.Equal(t, uint64(0), f.repKeeper.tags["gamma"].UsageCount,
		"hidden-post expiry must decrement tag usage")
}

// Author per-tag rep slash (Tier 0): an unappealed sentinel hide must deduct
// the configured AuthorRepSlash from the author's score in *each* tag the
// post carried. Asserts deduct fires for every tag and not for non-tagged
// posts.
func TestExpireHiddenPosts_SlashesAuthorRepPerTag(t *testing.T) {
	f := initFixture(t)
	cat := f.createTestCategory(t, "General")

	hiddenAt := int64(1_000_000)
	now := hiddenAt + types.DefaultHiddenExpiration + 1
	f.ctx = f.sdkCtx().WithBlockTime(time.Unix(now, 0))

	// Seed the post's tags so DecrementTagUsage doesn't fail before our slash
	// hook runs (we already insert tags before the hook in abci.go).
	f.repKeeper.tags = map[string]reptypes.Tag{
		"alpha": {Name: "alpha", UsageCount: 1},
		"bravo": {Name: "bravo", UsageCount: 1},
	}

	postID, err := f.keeper.PostSeq.Next(f.ctx)
	require.NoError(t, err)

	post := types.Post{
		PostId:     postID,
		CategoryId: cat.CategoryId,
		Author:     testCreator,
		Content:    "Hidden tagged post",
		CreatedAt:  hiddenAt - 100,
		Status:     types.PostStatus_POST_STATUS_HIDDEN,
		HiddenAt:   hiddenAt,
		Tags:       []string{"alpha", "bravo"},
	}
	require.NoError(t, f.keeper.Post.Set(f.ctx, postID, post))
	require.NoError(t, f.keeper.HideRecord.Set(f.ctx, postID, types.HideRecord{
		PostId:   postID,
		HiddenAt: hiddenAt,
	}))

	require.NoError(t, f.keeper.ExpireHiddenPosts(f.ctx, now))

	require.Len(t, f.repKeeper.deductCalls, 2, "expected one rep slash per tag")
	got := map[string]bool{}
	for _, call := range f.repKeeper.deductCalls {
		require.Equal(t, testCreator, call.Addr, "slash must target the post author")
		require.True(t, call.Amount.Equal(types.DefaultAuthorRepSlash),
			"slash amount must match params.AuthorRepSlash")
		got[call.Tag] = true
	}
	require.True(t, got["alpha"] && got["bravo"], "both tags must be slashed")
}

// Promoter MemberWarning (Tier 1): if the post was promoted via
// MsgMakePostPermanent by a different member, the unappealed-hide
// finalization must issue a MemberWarning against that promoter with the
// post id in the evidence list.
func TestExpireHiddenPosts_IssuesPromoterWarning(t *testing.T) {
	f := initFixture(t)
	cat := f.createTestCategory(t, "General")

	hiddenAt := int64(1_000_000)
	now := hiddenAt + types.DefaultHiddenExpiration + 1
	f.ctx = f.sdkCtx().WithBlockTime(time.Unix(now, 0))

	promoter := sdk.AccAddress([]byte("phoenix_promoter")).String()

	postID, err := f.keeper.PostSeq.Next(f.ctx)
	require.NoError(t, err)

	post := types.Post{
		PostId:      postID,
		CategoryId:  cat.CategoryId,
		Author:      testCreator,
		Content:     "Promoted but then hidden",
		CreatedAt:   hiddenAt - 100,
		Status:      types.PostStatus_POST_STATUS_HIDDEN,
		HiddenAt:    hiddenAt,
		PromotedBy:  promoter,
		PromotedAt:  hiddenAt - 50,
	}
	require.NoError(t, f.keeper.Post.Set(f.ctx, postID, post))
	require.NoError(t, f.keeper.HideRecord.Set(f.ctx, postID, types.HideRecord{
		PostId:   postID,
		HiddenAt: hiddenAt,
	}))

	require.NoError(t, f.keeper.ExpireHiddenPosts(f.ctx, now))

	require.Len(t, f.repKeeper.warningCalls, 1, "exactly one warning must be issued")
	w := f.repKeeper.warningCalls[0]
	require.Equal(t, promoter, w.Member)
	require.Equal(t, f.keeper.GetModuleAddress(), w.IssuedBy)
	require.Equal(t, "promoted_hidden_content", w.Reason)
	require.Equal(t, []uint64{postID}, w.EvidencePostIDs)
}

// Self-promote skip: if the post's PromotedBy equals the author (which
// shouldn't happen given the msg-server guard, but defense-in-depth here),
// no warning fires — Tier 1 only bites cross-member vouching.
func TestExpireHiddenPosts_NoWarningOnSelfPromote(t *testing.T) {
	f := initFixture(t)
	cat := f.createTestCategory(t, "General")

	hiddenAt := int64(1_000_000)
	now := hiddenAt + types.DefaultHiddenExpiration + 1
	f.ctx = f.sdkCtx().WithBlockTime(time.Unix(now, 0))

	postID, err := f.keeper.PostSeq.Next(f.ctx)
	require.NoError(t, err)

	post := types.Post{
		PostId:     postID,
		CategoryId: cat.CategoryId,
		Author:     testCreator,
		Content:    "Self-promoted hidden post",
		CreatedAt:  hiddenAt - 100,
		Status:     types.PostStatus_POST_STATUS_HIDDEN,
		HiddenAt:   hiddenAt,
		PromotedBy: testCreator, // same as Author
		PromotedAt: hiddenAt - 50,
	}
	require.NoError(t, f.keeper.Post.Set(f.ctx, postID, post))
	require.NoError(t, f.keeper.HideRecord.Set(f.ctx, postID, types.HideRecord{
		PostId:   postID,
		HiddenAt: hiddenAt,
	}))

	require.NoError(t, f.keeper.ExpireHiddenPosts(f.ctx, now))

	require.Empty(t, f.repKeeper.warningCalls, "self-promote must not trigger a warning")
}
