package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/types"
)

func TestRemoveReaction(t *testing.T) {
	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	creator2 := "sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y"

	t.Run("successful removal", func(t *testing.T) {
		k, msgServer, ctx, _ := setupMsgServer(t)

		postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Test Post",
			Body:    "Test body",
		})
		require.NoError(t, err)

		// Add a reaction first
		_, err = msgServer.React(ctx, &types.MsgReact{
			Creator:      creator2,
			PostId:       postResp.Id,
			ReplyId:      0,
			ReactionType: types.ReactionType_REACTION_TYPE_LIKE,
		})
		require.NoError(t, err)

		// Verify reaction exists
		_, found := k.GetReaction(ctx, postResp.Id, 0, creator2)
		require.True(t, found)

		// Remove it
		resp, err := msgServer.RemoveReaction(ctx, &types.MsgRemoveReaction{
			Creator: creator2,
			PostId:  postResp.Id,
			ReplyId: 0,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify reaction is gone
		_, found = k.GetReaction(ctx, postResp.Id, 0, creator2)
		require.False(t, found)
	})

	t.Run("reaction not found", func(t *testing.T) {
		_, msgServer, ctx, _ := setupMsgServer(t)

		postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Test Post",
			Body:    "Test body",
		})
		require.NoError(t, err)

		// Try to remove a reaction that does not exist
		_, err = msgServer.RemoveReaction(ctx, &types.MsgRemoveReaction{
			Creator: creator2,
			PostId:  postResp.Id,
			ReplyId: 0,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "no reaction found")
	})

	t.Run("verify reaction counts decremented after removal", func(t *testing.T) {
		k, msgServer, ctx, _ := setupMsgServer(t)

		postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Test Post",
			Body:    "Test body",
		})
		require.NoError(t, err)

		// Add two LIKE reactions from different users
		_, err = msgServer.React(ctx, &types.MsgReact{
			Creator:      creator,
			PostId:       postResp.Id,
			ReplyId:      0,
			ReactionType: types.ReactionType_REACTION_TYPE_LIKE,
		})
		require.NoError(t, err)

		_, err = msgServer.React(ctx, &types.MsgReact{
			Creator:      creator2,
			PostId:       postResp.Id,
			ReplyId:      0,
			ReactionType: types.ReactionType_REACTION_TYPE_LIKE,
		})
		require.NoError(t, err)

		// Verify count is 2
		counts := k.GetReactionCounts(ctx, postResp.Id, 0)
		require.Equal(t, uint64(2), counts.LikeCount)

		// Remove one reaction
		_, err = msgServer.RemoveReaction(ctx, &types.MsgRemoveReaction{
			Creator: creator2,
			PostId:  postResp.Id,
			ReplyId: 0,
		})
		require.NoError(t, err)

		// Verify count decremented to 1
		counts = k.GetReactionCounts(ctx, postResp.Id, 0)
		require.Equal(t, uint64(1), counts.LikeCount)
	})
}
