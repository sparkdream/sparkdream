package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/types"
)

func TestHideReply(t *testing.T) {
	k, msgServer, ctx, _ := setupMsgServer(t)

	postAuthor := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	replyAuthor := "sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y"

	// Create a post by postAuthor
	postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
		Creator: postAuthor,
		Title:   "Test Post",
		Body:    "Post body",
	})
	require.NoError(t, err)
	postID := postResp.Id

	// Create a reply by replyAuthor for the happy path test
	replyResp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
		Creator: replyAuthor,
		PostId:  postID,
		Body:    "Reply body",
	})
	require.NoError(t, err)
	activeReplyID := replyResp.Id

	// Create a reply then delete it to test hiding a non-active reply
	deleteReplyResp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
		Creator: replyAuthor,
		PostId:  postID,
		Body:    "Will be deleted",
	})
	require.NoError(t, err)
	deletedReplyID := deleteReplyResp.Id
	_, err = msgServer.DeleteReply(ctx, &types.MsgDeleteReply{Creator: replyAuthor, Id: deletedReplyID})
	require.NoError(t, err)

	// Create a reply for the unauthorized test (reply author tries to hide)
	unauthReplyResp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
		Creator: replyAuthor,
		PostId:  postID,
		Body:    "Auth test reply",
	})
	require.NoError(t, err)
	unauthReplyID := unauthReplyResp.Id

	// Capture reply count right before running hide tests
	// At this point: 1 active reply (activeReplyID) + 1 active reply (unauthReplyID) + 1 deleted reply = 2 visible
	post, found := k.GetPost(ctx, postID)
	require.True(t, found)
	replyCountBeforeHide := post.ReplyCount

	tests := []struct {
		name        string
		msg         *types.MsgHideReply
		expectError bool
		errContains string
	}{
		{
			name: "successful hide by post author",
			msg: &types.MsgHideReply{
				Creator: postAuthor,
				Id:      activeReplyID,
			},
			expectError: false,
		},
		{
			name: "reply not found",
			msg: &types.MsgHideReply{
				Creator: postAuthor,
				Id:      99999,
			},
			expectError: true,
			errContains: "doesn't exist",
		},
		{
			name: "reply not active",
			msg: &types.MsgHideReply{
				Creator: postAuthor,
				Id:      deletedReplyID,
			},
			expectError: true,
			errContains: "has been deleted",
		},
		{
			name: "not the post author (reply author tries to hide)",
			msg: &types.MsgHideReply{
				Creator: replyAuthor,
				Id:      unauthReplyID,
			},
			expectError: true,
			errContains: "only post author can hide a reply",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := msgServer.HideReply(ctx, tt.msg)
			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}

	// Verify reply status is HIDDEN and post.ReplyCount decremented
	t.Run("verify reply status is HIDDEN and ReplyCount decremented", func(t *testing.T) {
		reply, found := k.GetReply(ctx, activeReplyID)
		require.True(t, found)
		require.Equal(t, types.ReplyStatus_REPLY_STATUS_HIDDEN, reply.Status)
		require.Equal(t, postAuthor, reply.HiddenBy)
		require.NotZero(t, reply.HiddenAt)

		post, found := k.GetPost(ctx, postID)
		require.True(t, found)
		require.Equal(t, replyCountBeforeHide-1, post.ReplyCount)
	})
}
