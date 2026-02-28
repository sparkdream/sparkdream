package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/types"
)

func TestUnhideReply(t *testing.T) {
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

	// Create a reply, then hide it for the happy path test
	replyResp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
		Creator: replyAuthor,
		PostId:  postID,
		Body:    "Reply body",
	})
	require.NoError(t, err)
	hiddenReplyID := replyResp.Id
	_, err = msgServer.HideReply(ctx, &types.MsgHideReply{Creator: postAuthor, Id: hiddenReplyID})
	require.NoError(t, err)

	// Create an active (non-hidden) reply for the "not hidden" test case
	activeReplyResp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
		Creator: replyAuthor,
		PostId:  postID,
		Body:    "Active reply",
	})
	require.NoError(t, err)
	activeReplyID := activeReplyResp.Id

	// Create a reply, then hide it for the unauthorized test
	unauthReplyResp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
		Creator: replyAuthor,
		PostId:  postID,
		Body:    "Auth test reply",
	})
	require.NoError(t, err)
	unauthReplyID := unauthReplyResp.Id
	_, err = msgServer.HideReply(ctx, &types.MsgHideReply{Creator: postAuthor, Id: unauthReplyID})
	require.NoError(t, err)

	// Capture reply count right before running unhide tests
	post, found := k.GetPost(ctx, postID)
	require.True(t, found)
	replyCountBeforeUnhide := post.ReplyCount

	tests := []struct {
		name        string
		msg         *types.MsgUnhideReply
		expectError bool
		errContains string
	}{
		{
			name: "successful unhide by post author",
			msg: &types.MsgUnhideReply{
				Creator: postAuthor,
				Id:      hiddenReplyID,
			},
			expectError: false,
		},
		{
			name: "reply not found",
			msg: &types.MsgUnhideReply{
				Creator: postAuthor,
				Id:      99999,
			},
			expectError: true,
			errContains: "doesn't exist",
		},
		{
			name: "reply not hidden",
			msg: &types.MsgUnhideReply{
				Creator: postAuthor,
				Id:      activeReplyID,
			},
			expectError: true,
			errContains: "reply is not hidden",
		},
		{
			name: "not the post author (unauthorized)",
			msg: &types.MsgUnhideReply{
				Creator: replyAuthor,
				Id:      unauthReplyID,
			},
			expectError: true,
			errContains: "only post author can unhide a reply",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := msgServer.UnhideReply(ctx, tt.msg)
			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}

	// Verify reply status is ACTIVE and post.ReplyCount incremented back
	t.Run("verify reply status is ACTIVE and ReplyCount incremented", func(t *testing.T) {
		reply, found := k.GetReply(ctx, hiddenReplyID)
		require.True(t, found)
		require.Equal(t, types.ReplyStatus_REPLY_STATUS_ACTIVE, reply.Status)
		require.Empty(t, reply.HiddenBy)
		require.Zero(t, reply.HiddenAt)

		post, found := k.GetPost(ctx, postID)
		require.True(t, found)
		require.Equal(t, replyCountBeforeUnhide+1, post.ReplyCount)
	})
}
