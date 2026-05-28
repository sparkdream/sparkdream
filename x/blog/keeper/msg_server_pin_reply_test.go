package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// TestPinReply mirrors TestPinPost: strict Pin only sets the marker on
// already-permanent replies; ephemeral content is rejected with
// ErrCannotPinEphemeral.
func TestPinReply(t *testing.T) {
	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	createPermanentReply := func(t *testing.T) (keeper.Keeper, types.MsgServer, sdk.Context, uint64) {
		t.Helper()
		k, msgServer, ctx, _ := setupMsgServer(t)
		postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Test Post",
			Body:    "...",
		})
		require.NoError(t, err)
		replyResp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
			Creator: creator,
			PostId:  postResp.Id,
			Body:    "Permanent reply",
		})
		require.NoError(t, err)
		reply, found := k.GetReply(ctx, replyResp.Id)
		require.True(t, found)
		require.Equal(t, int64(0), reply.ExpiresAt, "test fixture must produce a permanent reply")
		return k, msgServer, ctx, replyResp.Id
	}

	t.Run("successful pin of permanent reply", func(t *testing.T) {
		k, msgServer, ctx, replyId := createPermanentReply(t)
		sdkCtx := sdk.UnwrapSDKContext(ctx)

		_, err := msgServer.PinReply(ctx, &types.MsgPinReply{Creator: creator, Id: replyId})
		require.NoError(t, err)

		reply, found := k.GetReply(ctx, replyId)
		require.True(t, found)
		require.Equal(t, int64(0), reply.ExpiresAt)
		require.Equal(t, creator, reply.PinnedBy)
		require.Equal(t, sdkCtx.BlockTime().Unix(), reply.PinnedAt)
	})

	t.Run("ephemeral reply rejected with ErrCannotPinEphemeral", func(t *testing.T) {
		k, msgServer, ctx, replyId := createPermanentReply(t)
		sdkCtx := sdk.UnwrapSDKContext(ctx)

		reply, _ := k.GetReply(ctx, replyId)
		reply.ExpiresAt = sdkCtx.BlockTime().Unix() + 604800
		k.SetReply(ctx, reply)
		k.AddToExpiryIndex(ctx, reply.ExpiresAt, "reply", reply.Id)
		k.AddEphemeralAuthorIndex(ctx, creator, keeper.EphemeralKindReply, reply.Id)

		_, err := msgServer.PinReply(ctx, &types.MsgPinReply{Creator: creator, Id: replyId})
		require.ErrorIs(t, err, types.ErrCannotPinEphemeral)
	})

	t.Run("reply not found", func(t *testing.T) {
		_, msgServer, ctx, _ := setupMsgServer(t)

		_, err := msgServer.PinReply(ctx, &types.MsgPinReply{Creator: creator, Id: 9999})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("reply deleted rejected", func(t *testing.T) {
		k, msgServer, ctx, replyId := createPermanentReply(t)

		reply, _ := k.GetReply(ctx, replyId)
		reply.Status = types.ReplyStatus_REPLY_STATUS_DELETED
		k.SetReply(ctx, reply)

		_, err := msgServer.PinReply(ctx, &types.MsgPinReply{Creator: creator, Id: replyId})
		require.Error(t, err)
		require.Contains(t, err.Error(), "has been deleted")
	})

	t.Run("reply hidden rejected", func(t *testing.T) {
		k, msgServer, ctx, replyId := createPermanentReply(t)

		reply, _ := k.GetReply(ctx, replyId)
		reply.Status = types.ReplyStatus_REPLY_STATUS_HIDDEN
		k.SetReply(ctx, reply)

		_, err := msgServer.PinReply(ctx, &types.MsgPinReply{Creator: creator, Id: replyId})
		require.Error(t, err)
		require.Contains(t, err.Error(), "is hidden")
	})

	t.Run("reply already pinned", func(t *testing.T) {
		_, msgServer, ctx, replyId := createPermanentReply(t)

		_, err := msgServer.PinReply(ctx, &types.MsgPinReply{Creator: creator, Id: replyId})
		require.NoError(t, err)

		_, err = msgServer.PinReply(ctx, &types.MsgPinReply{Creator: creator, Id: replyId})
		require.ErrorIs(t, err, types.ErrAlreadyPinned)
	})
}
