package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestMsgServerEditPost(t *testing.T) {
	f := initFixture(t)

	t.Run("invalid creator address", func(t *testing.T) {
		msg := &types.MsgEditPost{
			Creator:    "invalid",
			PostId:     1,
			NewContent: "Updated content",
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("post not found", func(t *testing.T) {
		msg := &types.MsgEditPost{
			Creator:    testCreator,
			PostId:     999,
			NewContent: "Updated content",
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrPostNotFound)
	})

	t.Run("not post author", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		msg := &types.MsgEditPost{
			Creator:    testCreator2,
			PostId:     post.PostId,
			NewContent: "Updated content",
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotPostAuthor)
	})

	t.Run("cannot edit hidden post", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		post.Status = types.PostStatus_POST_STATUS_HIDDEN
		f.keeper.Post.Set(f.ctx, post.PostId, post)

		msg := &types.MsgEditPost{
			Creator:    testCreator,
			PostId:     post.PostId,
			NewContent: "Updated content",
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrCannotEditHiddenPost)
	})

	t.Run("cannot edit deleted post", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		post.Status = types.PostStatus_POST_STATUS_DELETED
		f.keeper.Post.Set(f.ctx, post.PostId, post)

		msg := &types.MsgEditPost{
			Creator:    testCreator,
			PostId:     post.PostId,
			NewContent: "Updated content",
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrCannotEditDeletedPost)
	})

	t.Run("cannot edit archived post", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		post.Status = types.PostStatus_POST_STATUS_ARCHIVED
		f.keeper.Post.Set(f.ctx, post.PostId, post)

		msg := &types.MsgEditPost{
			Creator:    testCreator,
			PostId:     post.PostId,
			NewContent: "Updated content",
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrPostArchived)
	})

	t.Run("empty new content", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		msg := &types.MsgEditPost{
			Creator:    testCreator,
			PostId:     post.PostId,
			NewContent: "",
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrEmptyContent)
	})

	t.Run("successful edit", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		msg := &types.MsgEditPost{
			Creator:    testCreator,
			PostId:     post.PostId,
			NewContent: "Updated content here",
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.NoError(t, err)

		// Verify post was updated
		updatedPost, err := f.keeper.Post.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.Equal(t, "Updated content here", updatedPost.Content)
		require.True(t, updatedPost.Edited)
	})

	t.Run("editing disabled", func(t *testing.T) {
		params := types.DefaultParams()
		params.EditingEnabled = false
		f.keeper.Params.Set(f.ctx, params)

		post := f.createTestPost(t, testCreator, 0, 0)

		msg := &types.MsgEditPost{
			Creator:    testCreator,
			PostId:     post.PostId,
			NewContent: "Updated content",
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrEditingDisabled)

		// Reset params
		f.keeper.Params.Set(f.ctx, types.DefaultParams())
	})
}
