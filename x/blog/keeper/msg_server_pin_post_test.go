package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// TestPinPost covers the post-rework strict Pin semantics: Pin only sets the
// pinned marker, requires the post to already be permanent, and rejects
// ephemeral content with ErrCannotPinEphemeral. The ephemeral→permanent
// lifecycle change moved to MsgMakePostPermanent (covered in its own test).
func TestPinPost(t *testing.T) {
	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	// createPermanentPost creates a permanent post and returns its id. The
	// mockRepKeeper used by setupMsgServer reports the creator as an active
	// member, so newly-created posts default to permanent (ExpiresAt=0).
	createPermanentPost := func(t *testing.T) (keeper.Keeper, types.MsgServer, sdk.Context, uint64) {
		t.Helper()
		k, msgServer, ctx, _ := setupMsgServer(t)
		resp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Permanent Post",
			Body:    "...",
		})
		require.NoError(t, err)
		post, found := k.GetPost(ctx, resp.Id)
		require.True(t, found)
		require.Equal(t, int64(0), post.ExpiresAt, "test fixture must produce a permanent post")
		return k, msgServer, ctx, resp.Id
	}

	t.Run("successful pin of permanent post", func(t *testing.T) {
		k, msgServer, ctx, postId := createPermanentPost(t)
		sdkCtx := sdk.UnwrapSDKContext(ctx)

		_, err := msgServer.PinPost(ctx, &types.MsgPinPost{Creator: creator, Id: postId})
		require.NoError(t, err)

		post, found := k.GetPost(ctx, postId)
		require.True(t, found)
		require.Equal(t, int64(0), post.ExpiresAt)
		require.Equal(t, creator, post.PinnedBy)
		require.Equal(t, sdkCtx.BlockTime().Unix(), post.PinnedAt)
	})

	t.Run("ephemeral post rejected with ErrCannotPinEphemeral", func(t *testing.T) {
		k, msgServer, ctx, postId := createPermanentPost(t)
		sdkCtx := sdk.UnwrapSDKContext(ctx)

		// Force ephemeral via direct keeper access.
		post, _ := k.GetPost(ctx, postId)
		post.ExpiresAt = sdkCtx.BlockTime().Unix() + 604800
		k.SetPost(ctx, post)
		k.AddToExpiryIndex(ctx, post.ExpiresAt, "post", post.Id)
		k.AddEphemeralAuthorIndex(ctx, creator, keeper.EphemeralKindPost, post.Id)

		_, err := msgServer.PinPost(ctx, &types.MsgPinPost{Creator: creator, Id: postId})
		require.ErrorIs(t, err, types.ErrCannotPinEphemeral)

		// Sanity: pin marker NOT set after rejection.
		post, _ = k.GetPost(ctx, postId)
		require.Empty(t, post.PinnedBy)
	})

	t.Run("post not found", func(t *testing.T) {
		_, msgServer, ctx, _ := setupMsgServer(t)

		_, err := msgServer.PinPost(ctx, &types.MsgPinPost{Creator: creator, Id: 9999})
		require.Error(t, err)
		require.Contains(t, err.Error(), "post not found")
	})

	t.Run("post deleted rejected", func(t *testing.T) {
		k, msgServer, ctx, postId := createPermanentPost(t)

		post, _ := k.GetPost(ctx, postId)
		post.Status = types.PostStatus_POST_STATUS_DELETED
		k.SetPost(ctx, post)

		_, err := msgServer.PinPost(ctx, &types.MsgPinPost{Creator: creator, Id: postId})
		require.Error(t, err)
		require.Contains(t, err.Error(), "has been deleted")
	})

	t.Run("post hidden rejected", func(t *testing.T) {
		k, msgServer, ctx, postId := createPermanentPost(t)

		post, _ := k.GetPost(ctx, postId)
		post.Status = types.PostStatus_POST_STATUS_HIDDEN
		k.SetPost(ctx, post)

		_, err := msgServer.PinPost(ctx, &types.MsgPinPost{Creator: creator, Id: postId})
		require.Error(t, err)
		require.Contains(t, err.Error(), "is hidden")
	})

	t.Run("post already pinned", func(t *testing.T) {
		_, msgServer, ctx, postId := createPermanentPost(t)

		_, err := msgServer.PinPost(ctx, &types.MsgPinPost{Creator: creator, Id: postId})
		require.NoError(t, err)

		_, err = msgServer.PinPost(ctx, &types.MsgPinPost{Creator: creator, Id: postId})
		require.ErrorIs(t, err, types.ErrAlreadyPinned)
	})
}
