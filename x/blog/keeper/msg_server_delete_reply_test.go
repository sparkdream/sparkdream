package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/types"
)

func TestDeleteReply(t *testing.T) {
	_, msgServer, ctx, _ := setupMsgServer(t)

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	otherAddr := "sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y"

	// Create a post
	postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
		Creator: creator,
		Title:   "Test Post",
		Body:    "Post body",
	})
	require.NoError(t, err)

	tests := []struct {
		name        string
		setup       func() (uint64, string) // returns (replyID, postCreator)
		msg         *types.MsgDeleteReply
		expectError bool
		errContains string
	}{
		{
			name: "successful delete by reply author",
			setup: func() (uint64, string) {
				resp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
					Creator: creator,
					PostId:  postResp.Id,
					Body:    "Reply to delete by author",
				})
				require.NoError(t, err)
				return resp.Id, creator
			},
			msg: &types.MsgDeleteReply{
				Creator: creator,
			},
			expectError: false,
		},
		{
			name: "successful delete by post author",
			setup: func() (uint64, string) {
				// otherAddr creates a reply on creator's post
				resp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
					Creator: otherAddr,
					PostId:  postResp.Id,
					Body:    "Reply to delete by post author",
				})
				require.NoError(t, err)
				return resp.Id, creator
			},
			msg: &types.MsgDeleteReply{
				Creator: creator, // post author deletes someone else's reply
			},
			expectError: false,
		},
		{
			name: "reply not found",
			msg: &types.MsgDeleteReply{
				Creator: creator,
				Id:      99999,
			},
			expectError: true,
			errContains: "doesn't exist",
		},
		{
			name: "reply already deleted",
			setup: func() (uint64, string) {
				resp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
					Creator: creator,
					PostId:  postResp.Id,
					Body:    "Reply to double-delete",
				})
				require.NoError(t, err)
				// Delete once
				_, err = msgServer.DeleteReply(ctx, &types.MsgDeleteReply{
					Creator: creator,
					Id:      resp.Id,
				})
				require.NoError(t, err)
				return resp.Id, creator
			},
			msg: &types.MsgDeleteReply{
				Creator: creator,
			},
			expectError: true,
			errContains: "reply is already deleted",
		},
		{
			name: "not reply author or post author (unauthorized)",
			setup: func() (uint64, string) {
				resp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
					Creator: creator,
					PostId:  postResp.Id,
					Body:    "Reply that unauthorized user tries to delete",
				})
				require.NoError(t, err)
				return resp.Id, creator
			},
			msg: &types.MsgDeleteReply{
				Creator: otherAddr, // neither reply author (creator) nor post author (creator)
			},
			expectError: true,
			errContains: "only reply author or post author can delete a reply",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				replyID, _ := tt.setup()
				tt.msg.Id = replyID
			}

			_, err := msgServer.DeleteReply(ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDeleteReplyStatusIsDeleted(t *testing.T) {
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
		Body:    "Reply to verify status",
	})
	require.NoError(t, err)

	// Verify reply is active
	reply, found := k.GetReply(ctx, replyResp.Id)
	require.True(t, found)
	require.Equal(t, types.ReplyStatus_REPLY_STATUS_ACTIVE, reply.Status)

	// Delete the reply
	_, err = msgServer.DeleteReply(ctx, &types.MsgDeleteReply{
		Creator: creator,
		Id:      replyResp.Id,
	})
	require.NoError(t, err)

	// Verify reply status is DELETED and body is cleared (tombstoned)
	reply, found = k.GetReply(ctx, replyResp.Id)
	require.True(t, found, "tombstoned reply should still exist in store")
	require.Equal(t, types.ReplyStatus_REPLY_STATUS_DELETED, reply.Status)
	require.Empty(t, reply.Body, "body should be cleared on tombstone")
}

func TestDeleteReplyDecrementsReplyCount(t *testing.T) {
	k, msgServer, ctx, _ := setupMsgServer(t)

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	// Create a post
	postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
		Creator: creator,
		Title:   "Test Post",
		Body:    "Post body",
	})
	require.NoError(t, err)

	// Create 3 replies
	replyIDs := make([]uint64, 3)
	for i := 0; i < 3; i++ {
		resp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
			Creator: creator,
			PostId:  postResp.Id,
			Body:    "Reply body",
		})
		require.NoError(t, err)
		replyIDs[i] = resp.Id
	}

	// Verify reply count is 3
	post, found := k.GetPost(ctx, postResp.Id)
	require.True(t, found)
	require.Equal(t, uint64(3), post.ReplyCount)

	// Delete one reply
	_, err = msgServer.DeleteReply(ctx, &types.MsgDeleteReply{
		Creator: creator,
		Id:      replyIDs[0],
	})
	require.NoError(t, err)

	// Verify reply count is decremented to 2
	post, found = k.GetPost(ctx, postResp.Id)
	require.True(t, found)
	require.Equal(t, uint64(2), post.ReplyCount)

	// Delete another reply
	_, err = msgServer.DeleteReply(ctx, &types.MsgDeleteReply{
		Creator: creator,
		Id:      replyIDs[1],
	})
	require.NoError(t, err)

	// Verify reply count is decremented to 1
	post, found = k.GetPost(ctx, postResp.Id)
	require.True(t, found)
	require.Equal(t, uint64(1), post.ReplyCount)
}
