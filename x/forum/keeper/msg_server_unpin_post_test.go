package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestMsgServerUnpinPost(t *testing.T) {
	f := initFixture(t)
	authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	t.Run("invalid creator address", func(t *testing.T) {
		msg := &types.MsgUnpinPost{
			Creator: "invalid",
			PostId:  1,
		}
		_, err := f.msgServer.UnpinPost(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("not governance authority", func(t *testing.T) {
		msg := &types.MsgUnpinPost{
			Creator: testCreator,
			PostId:  1,
		}
		_, err := f.msgServer.UnpinPost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotGovAuthority)
	})

	t.Run("post not found", func(t *testing.T) {
		msg := &types.MsgUnpinPost{
			Creator: authority,
			PostId:  999,
		}
		_, err := f.msgServer.UnpinPost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrPostNotFound)
	})

	t.Run("post not pinned", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		msg := &types.MsgUnpinPost{
			Creator: authority,
			PostId:  post.PostId,
		}
		_, err := f.msgServer.UnpinPost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotPinned)
	})

	t.Run("successful unpin", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		// Pin the post first
		post.Pinned = true
		post.PinnedBy = authority
		post.PinnedAt = f.sdkCtx().BlockTime().Unix()
		post.PinPriority = 5
		f.keeper.Post.Set(f.ctx, post.PostId, post)

		msg := &types.MsgUnpinPost{
			Creator: authority,
			PostId:  post.PostId,
		}
		_, err := f.msgServer.UnpinPost(f.ctx, msg)
		require.NoError(t, err)

		// Verify post is unpinned
		updatedPost, err := f.keeper.Post.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.False(t, updatedPost.Pinned)
		require.Empty(t, updatedPost.PinnedBy)
		require.Equal(t, int64(0), updatedPost.PinnedAt)
		require.Equal(t, uint64(0), updatedPost.PinPriority)
	})
}
