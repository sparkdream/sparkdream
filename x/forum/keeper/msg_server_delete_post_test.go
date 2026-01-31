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
