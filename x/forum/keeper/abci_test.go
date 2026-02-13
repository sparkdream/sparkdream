package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestPruneExpiredPosts(t *testing.T) {
	t.Run("prunes expired ephemeral posts", func(t *testing.T) {
		f := initFixture(t)
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
}
