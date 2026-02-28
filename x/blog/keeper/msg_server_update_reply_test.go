package keeper_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/types"
)

func TestUpdateReply(t *testing.T) {
	k, msgServer, ctx, _ := setupMsgServer(t)

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	otherAddr := "sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y"

	// Create a post and a reply
	postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
		Creator: creator,
		Title:   "Test Post",
		Body:    "Post body",
	})
	require.NoError(t, err)

	replyResp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
		Creator: creator,
		PostId:  postResp.Id,
		Body:    "Original reply body",
	})
	require.NoError(t, err)
	replyID := replyResp.Id

	tests := []struct {
		name        string
		setup       func() uint64 // returns replyID to use
		msg         *types.MsgUpdateReply
		expectError bool
		errContains string
	}{
		{
			name: "successful update",
			setup: func() uint64 {
				resp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
					Creator: creator,
					PostId:  postResp.Id,
					Body:    "Reply to update",
				})
				require.NoError(t, err)
				return resp.Id
			},
			msg: &types.MsgUpdateReply{
				Creator: creator,
				Body:    "Updated reply body",
			},
			expectError: false,
		},
		{
			name: "reply not found",
			msg: &types.MsgUpdateReply{
				Creator: creator,
				Id:      99999,
				Body:    "Update nonexistent reply",
			},
			expectError: true,
			errContains: "doesn't exist",
		},
		{
			name: "reply not active (deleted)",
			setup: func() uint64 {
				resp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
					Creator: creator,
					PostId:  postResp.Id,
					Body:    "Reply to delete then update",
				})
				require.NoError(t, err)
				reply, found := k.GetReply(ctx, resp.Id)
				require.True(t, found)
				reply.Status = types.ReplyStatus_REPLY_STATUS_DELETED
				k.SetReply(ctx, reply)
				return resp.Id
			},
			msg: &types.MsgUpdateReply{
				Creator: creator,
				Body:    "Update deleted reply",
			},
			expectError: true,
			errContains: "has been deleted",
		},
		{
			name: "not the reply author",
			msg: &types.MsgUpdateReply{
				Creator: otherAddr,
				Id:      replyID,
				Body:    "Unauthorized update",
			},
			expectError: true,
			errContains: "only reply author can update a reply",
		},
		{
			name: "empty body",
			msg: &types.MsgUpdateReply{
				Creator: creator,
				Id:      replyID,
				Body:    "",
			},
			expectError: true,
			errContains: "body cannot be empty",
		},
		{
			name: "body exceeds max length",
			msg: &types.MsgUpdateReply{
				Creator: creator,
				Id:      replyID,
				Body:    string(bytes.Repeat([]byte("a"), 2001)),
			},
			expectError: true,
			errContains: "body exceeds maximum length",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				targetReplyID := tt.setup()
				tt.msg.Id = targetReplyID
			}

			_, err := msgServer.UpdateReply(ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)

				// Verify reply was updated
				reply, found := k.GetReply(ctx, tt.msg.Id)
				require.True(t, found)
				require.Equal(t, tt.msg.Body, reply.Body)
				require.Equal(t, types.ReplyStatus_REPLY_STATUS_ACTIVE, reply.Status)
			}
		})
	}
}

func TestUpdateReplyEditedFlag(t *testing.T) {
	k, msgServer, ctx, _ := setupMsgServer(t)

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	// Create a post and a reply
	postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
		Creator: creator,
		Title:   "Test Post",
		Body:    "Post body",
	})
	require.NoError(t, err)

	replyResp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
		Creator: creator,
		PostId:  postResp.Id,
		Body:    "Original reply",
	})
	require.NoError(t, err)

	// Verify initial state: not edited
	reply, found := k.GetReply(ctx, replyResp.Id)
	require.True(t, found)
	require.False(t, reply.Edited, "reply should not be marked as edited initially")
	require.Equal(t, int64(0), reply.EditedAt, "editedAt should be zero initially")

	// Update the reply
	_, err = msgServer.UpdateReply(ctx, &types.MsgUpdateReply{
		Creator: creator,
		Id:      replyResp.Id,
		Body:    "Updated reply body",
	})
	require.NoError(t, err)

	// Verify edited flag and editedAt are set
	reply, found = k.GetReply(ctx, replyResp.Id)
	require.True(t, found)
	require.True(t, reply.Edited, "reply should be marked as edited after update")
	require.NotEqual(t, int64(0), reply.EditedAt, "editedAt should be set after update")
	require.Equal(t, "Updated reply body", reply.Body)
}
