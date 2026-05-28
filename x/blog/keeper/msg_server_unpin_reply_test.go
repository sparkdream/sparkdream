package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func TestUnpinReply(t *testing.T) {
	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	createAndPinReply := func(t *testing.T) (keeper.Keeper, types.MsgServer, sdk.Context, uint64) {
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
			Body:    "Pin me",
		})
		require.NoError(t, err)
		_, err = msgServer.PinReply(ctx, &types.MsgPinReply{Creator: creator, Id: replyResp.Id})
		require.NoError(t, err)
		return k, msgServer, ctx, replyResp.Id
	}

	t.Run("successful unpin", func(t *testing.T) {
		k, msgServer, ctx, id := createAndPinReply(t)

		_, err := msgServer.UnpinReply(ctx, &types.MsgUnpinReply{Creator: creator, Id: id})
		require.NoError(t, err)

		reply, found := k.GetReply(ctx, id)
		require.True(t, found)
		require.Equal(t, "", reply.PinnedBy)
		require.Equal(t, int64(0), reply.PinnedAt)
		require.Equal(t, int64(0), reply.ExpiresAt)
	})

	t.Run("reply not pinned", func(t *testing.T) {
		_, msgServer, ctx, _ := setupMsgServer(t)
		postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Test Post",
			Body:    "...",
		})
		require.NoError(t, err)
		replyResp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
			Creator: creator,
			PostId:  postResp.Id,
			Body:    "Not pinned",
		})
		require.NoError(t, err)

		_, err = msgServer.UnpinReply(ctx, &types.MsgUnpinReply{Creator: creator, Id: replyResp.Id})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotPinned)
	})

	t.Run("reply not found", func(t *testing.T) {
		_, msgServer, ctx, _ := setupMsgServer(t)
		_, err := msgServer.UnpinReply(ctx, &types.MsgUnpinReply{Creator: creator, Id: 9999})
		require.Error(t, err)
		require.Contains(t, err.Error(), "reply not found")
	})
}
