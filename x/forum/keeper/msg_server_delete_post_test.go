package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestMsgServerDeletePost(t *testing.T) {
	f := initFixture(t)

	t.Run("invalid creator address", func(t *testing.T) {
		msg := &types.MsgDeletePost{
			Creator: "invalid",
			PostId:  1,
		}
		_, err := f.msgServer.DeletePost(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("post not found", func(t *testing.T) {
		msg := &types.MsgDeletePost{
			Creator: testCreator,
			PostId:  999,
		}
		_, err := f.msgServer.DeletePost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrPostNotFound)
	})

	t.Run("not post author", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		msg := &types.MsgDeletePost{
			Creator: testCreator2,
			PostId:  post.PostId,
		}
		_, err := f.msgServer.DeletePost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotPostAuthor)
	})

	t.Run("cannot delete hidden post", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		// Mark as hidden
		post.Status = types.PostStatus_POST_STATUS_HIDDEN
		f.keeper.Post.Set(f.ctx, post.PostId, post)

		msg := &types.MsgDeletePost{
			Creator: testCreator,
			PostId:  post.PostId,
		}
		_, err := f.msgServer.DeletePost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrCannotDeleteHiddenPost)
	})

	t.Run("cannot delete already deleted post", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		// Mark as deleted
		post.Status = types.PostStatus_POST_STATUS_DELETED
		f.keeper.Post.Set(f.ctx, post.PostId, post)

		msg := &types.MsgDeletePost{
			Creator: testCreator,
			PostId:  post.PostId,
		}
		_, err := f.msgServer.DeletePost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrPostDeleted)
	})

	t.Run("cannot delete archived post", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		// Mark as archived
		post.Status = types.PostStatus_POST_STATUS_ARCHIVED
		f.keeper.Post.Set(f.ctx, post.PostId, post)

		msg := &types.MsgDeletePost{
			Creator: testCreator,
			PostId:  post.PostId,
		}
		_, err := f.msgServer.DeletePost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrPostArchived)
	})

	t.Run("successful deletion", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		msg := &types.MsgDeletePost{
			Creator: testCreator,
			PostId:  post.PostId,
		}
		_, err := f.msgServer.DeletePost(f.ctx, msg)
		require.NoError(t, err)

		// Verify post was soft deleted
		updatedPost, err := f.keeper.Post.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.Equal(t, types.PostStatus_POST_STATUS_DELETED, updatedPost.Status)
		require.Equal(t, "[deleted]", updatedPost.Content)
	})
}

// Regression: DeletePost must call DecrementTagUsage for every tag the post
// carried — without this, repeated create/delete cycles inflate UsageCount
// in the rep registry and ExpireTags loses its grip on idle tags. Cousin of
// the edit-side TestEditPostTagsDiff_DecrementsDroppedTags fix.
func TestDeletePostDecrementsTagUsage(t *testing.T) {
	t.Run("multi-tag delete decrements every tag exactly once", func(t *testing.T) {
		f := initFixture(t)
		f.createTestTag(t, "golang")
		f.createTestTag(t, "cosmos-sdk")
		post := f.createTestPost(t, testCreator, 0, 0)

		// Attach two tags via EditPost so the rep mock's tag UsageCount goes
		// to 1 for each — this is the state we're asserting against.
		_, err := f.msgServer.EditPost(f.ctx, &types.MsgEditPost{
			Creator: testCreator, PostId: post.PostId,
			NewContent: "with tags",
			Tags:       []string{"golang", "cosmos-sdk"},
		})
		require.NoError(t, err)
		require.Equal(t, uint64(1), f.repKeeper.tags["golang"].UsageCount)
		require.Equal(t, uint64(1), f.repKeeper.tags["cosmos-sdk"].UsageCount)

		_, err = f.msgServer.DeletePost(f.ctx, &types.MsgDeletePost{
			Creator: testCreator, PostId: post.PostId,
		})
		require.NoError(t, err)

		// Both tags decremented exactly once — net usage back to baseline.
		require.Equal(t, uint64(0), f.repKeeper.tags["golang"].UsageCount,
			"delete must decrement every tag the post carried")
		require.Equal(t, uint64(0), f.repKeeper.tags["cosmos-sdk"].UsageCount,
			"delete must decrement every tag the post carried")

		// Post's own Tags must also be cleared on tombstone, otherwise a
		// subsequent gov-reverse-style path could try to re-diff a deleted
		// post and double-count.
		updated, err := f.keeper.Post.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.Nil(t, updated.Tags, "deleted post must drop its Tags reference")
	})

	t.Run("untagged delete does not call DecrementTagUsage", func(t *testing.T) {
		f := initFixture(t)
		post := f.createTestPost(t, testCreator, 0, 0)

		_, err := f.msgServer.DeletePost(f.ctx, &types.MsgDeletePost{
			Creator: testCreator, PostId: post.PostId,
		})
		require.NoError(t, err)

		// No tag rows touched.
		require.Empty(t, f.repKeeper.tags,
			"untagged post delete must not synthesize any tag activity")
	})
}
