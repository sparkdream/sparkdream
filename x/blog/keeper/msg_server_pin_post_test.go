package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func TestPinPost(t *testing.T) {
	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	// createEphemeralPost creates a post and then manually makes it ephemeral
	// by setting ExpiresAt and adding to the expiry index.
	createEphemeralPost := func(t *testing.T) (keeper.Keeper, types.MsgServer, sdk.Context, uint64) {
		t.Helper()
		k, msgServer, ctx, _ := setupMsgServer(t)
		sdkCtx := sdk.UnwrapSDKContext(ctx)

		resp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Ephemeral Post",
			Body:    "This post will be made ephemeral",
		})
		require.NoError(t, err)

		// Make the post ephemeral by setting ExpiresAt
		post, found := k.GetPost(ctx, resp.Id)
		require.True(t, found)
		post.ExpiresAt = sdkCtx.BlockTime().Unix() + 604800
		k.SetPost(ctx, post)
		k.AddToExpiryIndex(ctx, post.ExpiresAt, "post", post.Id)

		return k, msgServer, ctx, resp.Id
	}

	t.Run("successful pin of ephemeral post", func(t *testing.T) {
		k, msgServer, ctx, postId := createEphemeralPost(t)

		resp, err := msgServer.PinPost(ctx, &types.MsgPinPost{
			Creator: creator,
			Id:      postId,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify post is now permanent and pinned
		post, found := k.GetPost(ctx, postId)
		require.True(t, found)
		require.Equal(t, int64(0), post.ExpiresAt)
		require.Equal(t, creator, post.PinnedBy)
		require.NotZero(t, post.PinnedAt)
	})

	t.Run("post not found", func(t *testing.T) {
		_, msgServer, ctx, _ := setupMsgServer(t)

		_, err := msgServer.PinPost(ctx, &types.MsgPinPost{
			Creator: creator,
			Id:      9999,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "post not found")
	})

	t.Run("post not active", func(t *testing.T) {
		k, msgServer, ctx, postId := createEphemeralPost(t)

		// Set post status to deleted
		post, found := k.GetPost(ctx, postId)
		require.True(t, found)
		post.Status = types.PostStatus_POST_STATUS_DELETED
		k.SetPost(ctx, post)

		_, err := msgServer.PinPost(ctx, &types.MsgPinPost{
			Creator: creator,
			Id:      postId,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "has been deleted")
	})

	t.Run("post is permanent not ephemeral", func(t *testing.T) {
		_, msgServer, ctx, _ := setupMsgServer(t)

		// Create a permanent post (active member, ExpiresAt=0)
		resp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Permanent Post",
			Body:    "This is a permanent post",
		})
		require.NoError(t, err)

		_, err = msgServer.PinPost(ctx, &types.MsgPinPost{
			Creator: creator,
			Id:      resp.Id,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not ephemeral")
	})

	t.Run("post already expired", func(t *testing.T) {
		k, msgServer, ctx, _ := setupMsgServer(t)
		sdkCtx := sdk.UnwrapSDKContext(ctx)

		resp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Expired Post",
			Body:    "This post will be expired",
		})
		require.NoError(t, err)

		// Set ExpiresAt to a time in the past
		post, found := k.GetPost(ctx, resp.Id)
		require.True(t, found)
		post.ExpiresAt = sdkCtx.BlockTime().Unix() - 1
		k.SetPost(ctx, post)

		_, err = msgServer.PinPost(ctx, &types.MsgPinPost{
			Creator: creator,
			Id:      resp.Id,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "expired")
	})

	t.Run("post already pinned", func(t *testing.T) {
		k, msgServer, ctx, postId := createEphemeralPost(t)

		// Pin it once
		_, err := msgServer.PinPost(ctx, &types.MsgPinPost{
			Creator: creator,
			Id:      postId,
		})
		require.NoError(t, err)

		// PinnedBy is now set. Re-set ExpiresAt to a future value so
		// the ephemeral check passes and we reach the already-pinned check.
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		post, found := k.GetPost(ctx, postId)
		require.True(t, found)
		post.ExpiresAt = sdkCtx.BlockTime().Unix() + 604800
		k.SetPost(ctx, post)

		_, err = msgServer.PinPost(ctx, &types.MsgPinPost{
			Creator: creator,
			Id:      postId,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "already pinned")
	})

	t.Run("verify ExpiresAt becomes 0 and PinnedBy is set after pin", func(t *testing.T) {
		k, msgServer, ctx, postId := createEphemeralPost(t)
		sdkCtx := sdk.UnwrapSDKContext(ctx)

		// Confirm ephemeral before pinning
		post, found := k.GetPost(ctx, postId)
		require.True(t, found)
		require.NotZero(t, post.ExpiresAt)
		require.Empty(t, post.PinnedBy)

		_, err := msgServer.PinPost(ctx, &types.MsgPinPost{
			Creator: creator,
			Id:      postId,
		})
		require.NoError(t, err)

		// Confirm permanent after pinning
		post, found = k.GetPost(ctx, postId)
		require.True(t, found)
		require.Equal(t, int64(0), post.ExpiresAt)
		require.Equal(t, creator, post.PinnedBy)
		require.Equal(t, sdkCtx.BlockTime().Unix(), post.PinnedAt)
	})
}
