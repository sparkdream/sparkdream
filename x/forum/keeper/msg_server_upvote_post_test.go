package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestMsgServerUpvotePost(t *testing.T) {
	f := initFixture(t)

	t.Run("invalid creator address", func(t *testing.T) {
		msg := &types.MsgUpvotePost{
			Creator: "invalid",
			PostId:  1,
		}
		_, err := f.msgServer.UpvotePost(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("post not found", func(t *testing.T) {
		msg := &types.MsgUpvotePost{
			Creator: testCreator,
			PostId:  999,
		}
		_, err := f.msgServer.UpvotePost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrPostNotFound)
	})

	t.Run("cannot vote on hidden post", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		post.Status = types.PostStatus_POST_STATUS_HIDDEN
		f.keeper.Post.Set(f.ctx, post.PostId, post)

		msg := &types.MsgUpvotePost{
			Creator: testCreator2,
			PostId:  post.PostId,
		}
		_, err := f.msgServer.UpvotePost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrPostAlreadyHidden)
	})

	t.Run("cannot vote on deleted post", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		post.Status = types.PostStatus_POST_STATUS_DELETED
		f.keeper.Post.Set(f.ctx, post.PostId, post)

		msg := &types.MsgUpvotePost{
			Creator: testCreator2,
			PostId:  post.PostId,
		}
		_, err := f.msgServer.UpvotePost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrPostDeleted)
	})

	t.Run("cannot vote on own post", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		msg := &types.MsgUpvotePost{
			Creator: testCreator,
			PostId:  post.PostId,
		}
		_, err := f.msgServer.UpvotePost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrCannotVoteOwnPost)
	})

	t.Run("successful upvote", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		initialUpvotes := post.UpvoteCount

		msg := &types.MsgUpvotePost{
			Creator: testCreator2,
			PostId:  post.PostId,
		}
		_, err := f.msgServer.UpvotePost(f.ctx, msg)
		require.NoError(t, err)

		// Verify upvote count increased
		updatedPost, err := f.keeper.Post.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.Equal(t, initialUpvotes+1, updatedPost.UpvoteCount)
	})

	t.Run("reactions disabled", func(t *testing.T) {
		params := types.DefaultParams()
		params.ReactionsEnabled = false
		f.keeper.Params.Set(f.ctx, params)

		post := f.createTestPost(t, testCreator, 0, 0)

		msg := &types.MsgUpvotePost{
			Creator: testCreator2,
			PostId:  post.PostId,
		}
		_, err := f.msgServer.UpvotePost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrReactionsDisabled)

		// Reset params
		f.keeper.Params.Set(f.ctx, types.DefaultParams())
	})
}
