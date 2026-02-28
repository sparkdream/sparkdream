package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func TestPinReply(t *testing.T) {
	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	// createPostAndEphemeralReply creates a post with replies enabled, then a reply
	// that is manually made ephemeral by setting ExpiresAt.
	createPostAndEphemeralReply := func(t *testing.T) (keeper.Keeper, types.MsgServer, sdk.Context, uint64, uint64) {
		t.Helper()
		k, msgServer, ctx, _ := setupMsgServer(t)
		sdkCtx := sdk.UnwrapSDKContext(ctx)

		postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Test Post",
			Body:    "Post body for reply test",
		})
		require.NoError(t, err)

		replyResp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
			Creator: creator,
			PostId:  postResp.Id,
			Body:    "Test reply body",
		})
		require.NoError(t, err)

		// Make the reply ephemeral
		reply, found := k.GetReply(ctx, replyResp.Id)
		require.True(t, found)
		reply.ExpiresAt = sdkCtx.BlockTime().Unix() + 604800
		k.SetReply(ctx, reply)
		k.AddToExpiryIndex(ctx, reply.ExpiresAt, "reply", reply.Id)

		return k, msgServer, ctx, postResp.Id, replyResp.Id
	}

	t.Run("successful pin of ephemeral reply", func(t *testing.T) {
		k, msgServer, ctx, _, replyId := createPostAndEphemeralReply(t)

		resp, err := msgServer.PinReply(ctx, &types.MsgPinReply{
			Creator: creator,
			Id:      replyId,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify reply is now permanent and pinned
		reply, found := k.GetReply(ctx, replyId)
		require.True(t, found)
		require.Equal(t, int64(0), reply.ExpiresAt)
		require.Equal(t, creator, reply.PinnedBy)
		require.NotZero(t, reply.PinnedAt)
	})

	t.Run("reply not found", func(t *testing.T) {
		_, msgServer, ctx, _ := setupMsgServer(t)

		_, err := msgServer.PinReply(ctx, &types.MsgPinReply{
			Creator: creator,
			Id:      9999,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "reply")
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("reply not active", func(t *testing.T) {
		k, msgServer, ctx, _, replyId := createPostAndEphemeralReply(t)

		// Set reply status to deleted
		reply, found := k.GetReply(ctx, replyId)
		require.True(t, found)
		reply.Status = types.ReplyStatus_REPLY_STATUS_DELETED
		k.SetReply(ctx, reply)

		_, err := msgServer.PinReply(ctx, &types.MsgPinReply{
			Creator: creator,
			Id:      replyId,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "has been deleted")
	})

	t.Run("reply is permanent not ephemeral", func(t *testing.T) {
		_, msgServer, ctx, _ := setupMsgServer(t)

		// Create post and reply (reply will be permanent since mockRepKeeper returns true)
		postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Test Post",
			Body:    "Post body",
		})
		require.NoError(t, err)

		replyResp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
			Creator: creator,
			PostId:  postResp.Id,
			Body:    "Permanent reply",
		})
		require.NoError(t, err)

		_, err = msgServer.PinReply(ctx, &types.MsgPinReply{
			Creator: creator,
			Id:      replyResp.Id,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not ephemeral")
	})

	t.Run("reply already expired", func(t *testing.T) {
		k, msgServer, ctx, _, replyId := createPostAndEphemeralReply(t)
		sdkCtx := sdk.UnwrapSDKContext(ctx)

		// Set ExpiresAt to a time in the past
		reply, found := k.GetReply(ctx, replyId)
		require.True(t, found)
		reply.ExpiresAt = sdkCtx.BlockTime().Unix() - 1
		k.SetReply(ctx, reply)

		_, err := msgServer.PinReply(ctx, &types.MsgPinReply{
			Creator: creator,
			Id:      replyId,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "expired")
	})

	t.Run("reply already pinned", func(t *testing.T) {
		k, msgServer, ctx, _, replyId := createPostAndEphemeralReply(t)

		// Pin it once
		_, err := msgServer.PinReply(ctx, &types.MsgPinReply{
			Creator: creator,
			Id:      replyId,
		})
		require.NoError(t, err)

		// Re-set ExpiresAt so it passes the ephemeral check; already-pinned check is next
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		reply, found := k.GetReply(ctx, replyId)
		require.True(t, found)
		reply.ExpiresAt = sdkCtx.BlockTime().Unix() + 604800
		k.SetReply(ctx, reply)

		_, err = msgServer.PinReply(ctx, &types.MsgPinReply{
			Creator: creator,
			Id:      replyId,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "already pinned")
	})

	t.Run("verify ExpiresAt becomes 0 and PinnedBy set after pin", func(t *testing.T) {
		k, msgServer, ctx, _, replyId := createPostAndEphemeralReply(t)
		sdkCtx := sdk.UnwrapSDKContext(ctx)

		// Confirm ephemeral before pinning
		reply, found := k.GetReply(ctx, replyId)
		require.True(t, found)
		require.NotZero(t, reply.ExpiresAt)
		require.Empty(t, reply.PinnedBy)

		_, err := msgServer.PinReply(ctx, &types.MsgPinReply{
			Creator: creator,
			Id:      replyId,
		})
		require.NoError(t, err)

		// Confirm permanent after pinning
		reply, found = k.GetReply(ctx, replyId)
		require.True(t, found)
		require.Equal(t, int64(0), reply.ExpiresAt)
		require.Equal(t, creator, reply.PinnedBy)
		require.Equal(t, sdkCtx.BlockTime().Unix(), reply.PinnedAt)
	})
}
