package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// TestMakeReplyPermanent mirrors TestMakePostPermanent for replies.
func TestMakeReplyPermanent(t *testing.T) {
	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	createEphemeralReply := func(t *testing.T) (keeper.Keeper, types.MsgServer, sdk.Context, uint64) {
		t.Helper()
		k, msgServer, ctx, _ := setupMsgServer(t)
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Parent post",
			Body:    "...",
		})
		require.NoError(t, err)
		replyResp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
			Creator: creator,
			PostId:  postResp.Id,
			Body:    "Ephemeral reply",
		})
		require.NoError(t, err)
		reply, _ := k.GetReply(ctx, replyResp.Id)
		reply.ExpiresAt = sdkCtx.BlockTime().Unix() + 604800
		k.SetReply(ctx, reply)
		k.AddToExpiryIndex(ctx, reply.ExpiresAt, "reply", reply.Id)
		k.AddEphemeralAuthorIndex(ctx, creator, keeper.EphemeralKindReply, reply.Id)
		return k, msgServer, ctx, replyResp.Id
	}

	t.Run("successful promotion of ephemeral reply", func(t *testing.T) {
		k, msgServer, ctx, replyId := createEphemeralReply(t)

		_, err := msgServer.MakeReplyPermanent(ctx, &types.MsgMakeReplyPermanent{
			Creator: creator,
			Id:      replyId,
		})
		require.NoError(t, err)

		reply, found := k.GetReply(ctx, replyId)
		require.True(t, found)
		require.Equal(t, int64(0), reply.ExpiresAt)
		require.Empty(t, reply.PinnedBy)
		require.Equal(t, int64(0), reply.PinnedAt)
	})

	t.Run("idempotent on already-permanent reply", func(t *testing.T) {
		_, msgServer, ctx, _ := setupMsgServer(t)
		postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Permanent",
			Body:    "...",
		})
		require.NoError(t, err)
		replyResp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
			Creator: creator,
			PostId:  postResp.Id,
			Body:    "Permanent reply",
		})
		require.NoError(t, err)

		_, err = msgServer.MakeReplyPermanent(ctx, &types.MsgMakeReplyPermanent{
			Creator: creator,
			Id:      replyResp.Id,
		})
		require.NoError(t, err)
	})

	t.Run("rejects reply not found", func(t *testing.T) {
		_, msgServer, ctx, _ := setupMsgServer(t)
		_, err := msgServer.MakeReplyPermanent(ctx, &types.MsgMakeReplyPermanent{
			Creator: creator,
			Id:      9999,
		})
		require.ErrorIs(t, err, types.ErrReplyNotFound)
	})

	t.Run("rejects deleted reply", func(t *testing.T) {
		k, msgServer, ctx, replyId := createEphemeralReply(t)
		reply, _ := k.GetReply(ctx, replyId)
		reply.Status = types.ReplyStatus_REPLY_STATUS_DELETED
		k.SetReply(ctx, reply)

		_, err := msgServer.MakeReplyPermanent(ctx, &types.MsgMakeReplyPermanent{
			Creator: creator,
			Id:      replyId,
		})
		require.ErrorIs(t, err, types.ErrReplyDeleted)
	})

	t.Run("rejects already-expired ephemeral reply", func(t *testing.T) {
		k, msgServer, ctx, replyId := createEphemeralReply(t)
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		reply, _ := k.GetReply(ctx, replyId)
		reply.ExpiresAt = sdkCtx.BlockTime().Unix() - 1
		k.SetReply(ctx, reply)

		_, err := msgServer.MakeReplyPermanent(ctx, &types.MsgMakeReplyPermanent{
			Creator: creator,
			Id:      replyId,
		})
		require.ErrorIs(t, err, types.ErrReplyExpired)
	})
}
