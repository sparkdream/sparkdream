package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestMsgServerPinPost(t *testing.T) {
	f := initFixture(t)
	authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	t.Run("invalid creator address", func(t *testing.T) {
		msg := &types.MsgPinPost{
			Creator:  "invalid",
			PostId:   1,
			Priority: 1,
		}
		_, err := f.msgServer.PinPost(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("not governance authority", func(t *testing.T) {
		msg := &types.MsgPinPost{
			Creator:  testCreator,
			PostId:   1,
			Priority: 1,
		}
		_, err := f.msgServer.PinPost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotGovAuthority)
	})

	t.Run("post not found", func(t *testing.T) {
		msg := &types.MsgPinPost{
			Creator:  authority,
			PostId:   999,
			Priority: 1,
		}
		_, err := f.msgServer.PinPost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrPostNotFound)
	})

	t.Run("cannot pin reply (not root post)", func(t *testing.T) {
		rootPost := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator, rootPost.PostId, 0)

		msg := &types.MsgPinPost{
			Creator:  authority,
			PostId:   reply.PostId,
			Priority: 1,
		}
		_, err := f.msgServer.PinPost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotRootPost)
	})

	t.Run("cannot pin deleted post", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		post.Status = types.PostStatus_POST_STATUS_DELETED
		f.keeper.Post.Set(f.ctx, post.PostId, post)

		msg := &types.MsgPinPost{
			Creator:  authority,
			PostId:   post.PostId,
			Priority: 1,
		}
		_, err := f.msgServer.PinPost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrPostStatus)
	})

	t.Run("cannot pin hidden post", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		post.Status = types.PostStatus_POST_STATUS_HIDDEN
		f.keeper.Post.Set(f.ctx, post.PostId, post)

		msg := &types.MsgPinPost{
			Creator:  authority,
			PostId:   post.PostId,
			Priority: 1,
		}
		_, err := f.msgServer.PinPost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrPostStatus)
	})

	t.Run("already pinned", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		post.Pinned = true
		f.keeper.Post.Set(f.ctx, post.PostId, post)

		msg := &types.MsgPinPost{
			Creator:  authority,
			PostId:   post.PostId,
			Priority: 1,
		}
		_, err := f.msgServer.PinPost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrAlreadyPinned)
	})

	t.Run("successful pin", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		msg := &types.MsgPinPost{
			Creator:  authority,
			PostId:   post.PostId,
			Priority: 5,
		}
		_, err := f.msgServer.PinPost(f.ctx, msg)
		require.NoError(t, err)

		// Verify post is pinned
		updatedPost, err := f.keeper.Post.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.True(t, updatedPost.Pinned)
		require.Equal(t, authority, updatedPost.PinnedBy)
		require.Equal(t, uint64(5), updatedPost.PinPriority)
	})
}
