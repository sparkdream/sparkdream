package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/types"
)

func TestUnpinPost(t *testing.T) {
	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	t.Run("successful unpin of permanent-pinned post", func(t *testing.T) {
		k, msgServer, ctx, _ := setupMsgServer(t)

		// Create permanent post then pin (just sets marker, ExpiresAt stays 0).
		resp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Pin me",
			Body:    "...",
		})
		require.NoError(t, err)
		_, err = msgServer.PinPost(ctx, &types.MsgPinPost{Creator: creator, Id: resp.Id})
		require.NoError(t, err)

		_, err = msgServer.UnpinPost(ctx, &types.MsgUnpinPost{Creator: creator, Id: resp.Id})
		require.NoError(t, err)

		post, found := k.GetPost(ctx, resp.Id)
		require.True(t, found)
		require.Equal(t, "", post.PinnedBy)
		require.Equal(t, int64(0), post.PinnedAt)
		// Permanence is preserved — unpin must not re-introduce an expiry.
		require.Equal(t, int64(0), post.ExpiresAt)
	})

	t.Run("unpin of post promoted via MakePostPermanent stays permanent", func(t *testing.T) {
		// Regression coverage for the strict-separation rework: promote an
		// ephemeral post via MakePostPermanent, then Pin/Unpin, and confirm
		// Unpin does not re-introduce an expiry.
		k, msgServer, ctx, _ := setupMsgServer(t)
		resp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Ephemeral then promoted",
			Body:    "...",
		})
		require.NoError(t, err)

		// Force ephemeral via direct keeper access (matches the test fixture
		// pattern used elsewhere).
		post, _ := k.GetPost(ctx, resp.Id)
		post.ExpiresAt = 1 << 32
		k.SetPost(ctx, post)
		k.AddToExpiryIndex(ctx, post.ExpiresAt, "post", post.Id)

		_, err = msgServer.MakePostPermanent(ctx, &types.MsgMakePostPermanent{Creator: creator, Id: resp.Id})
		require.NoError(t, err)

		_, err = msgServer.PinPost(ctx, &types.MsgPinPost{Creator: creator, Id: resp.Id})
		require.NoError(t, err)

		_, err = msgServer.UnpinPost(ctx, &types.MsgUnpinPost{Creator: creator, Id: resp.Id})
		require.NoError(t, err)

		post, _ = k.GetPost(ctx, resp.Id)
		require.Equal(t, "", post.PinnedBy)
		require.Equal(t, int64(0), post.ExpiresAt)
	})

	t.Run("post not pinned", func(t *testing.T) {
		_, msgServer, ctx, _ := setupMsgServer(t)
		resp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Not pinned",
			Body:    "...",
		})
		require.NoError(t, err)
		_, err = msgServer.UnpinPost(ctx, &types.MsgUnpinPost{Creator: creator, Id: resp.Id})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotPinned)
	})

	t.Run("post not found", func(t *testing.T) {
		_, msgServer, ctx, _ := setupMsgServer(t)
		_, err := msgServer.UnpinPost(ctx, &types.MsgUnpinPost{Creator: creator, Id: 9999})
		require.Error(t, err)
		require.Contains(t, err.Error(), "post not found")
	})
}
