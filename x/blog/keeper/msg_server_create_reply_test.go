package keeper_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/types"
)

func TestCreateReply(t *testing.T) {
	k, msgServer, ctx, _ := setupMsgServer(t)

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	// Create a post to reply to
	createPostResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
		Creator: creator,
		Title:   "Test Post",
		Body:    "This is a test post body",
	})
	require.NoError(t, err)
	postID := createPostResp.Id

	tests := []struct {
		name        string
		setup       func() uint64 // returns postID to use
		msg         *types.MsgCreateReply
		expectError bool
		errContains string
	}{
		{
			name: "successful reply to existing post",
			msg: &types.MsgCreateReply{
				Creator: creator,
				PostId:  postID,
				Body:    "This is a reply",
			},
			expectError: false,
		},
		{
			name: "invalid creator address",
			msg: &types.MsgCreateReply{
				Creator: "invalid-address",
				PostId:  postID,
				Body:    "This should fail",
			},
			expectError: true,
			errContains: "invalid creator address",
		},
		{
			name: "post not found",
			msg: &types.MsgCreateReply{
				Creator: creator,
				PostId:  99999,
				Body:    "Reply to nonexistent post",
			},
			expectError: true,
			errContains: "doesn't exist",
		},
		{
			name: "post not active (deleted)",
			setup: func() uint64 {
				resp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
					Creator: creator,
					Title:   "Deleted Post",
					Body:    "Will be deleted",
				})
				require.NoError(t, err)
				post, found := k.GetPost(ctx, resp.Id)
				require.True(t, found)
				post.Status = types.PostStatus_POST_STATUS_DELETED
				k.SetPost(ctx, post)
				return resp.Id
			},
			msg: &types.MsgCreateReply{
				Creator: creator,
				Body:    "Reply to deleted post",
			},
			expectError: true,
			errContains: "has been deleted",
		},
		{
			name: "replies disabled",
			setup: func() uint64 {
				resp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
					Creator: creator,
					Title:   "No Replies Post",
					Body:    "Replies disabled here",
				})
				require.NoError(t, err)
				post, found := k.GetPost(ctx, resp.Id)
				require.True(t, found)
				post.RepliesEnabled = false
				k.SetPost(ctx, post)
				return resp.Id
			},
			msg: &types.MsgCreateReply{
				Creator: creator,
				Body:    "Reply to no-replies post",
			},
			expectError: true,
			errContains: "replies are disabled for this post",
		},
		{
			name: "empty body",
			msg: &types.MsgCreateReply{
				Creator: creator,
				PostId:  postID,
				Body:    "",
			},
			expectError: true,
			errContains: "body cannot be empty",
		},
		{
			name: "body exceeds max length",
			msg: &types.MsgCreateReply{
				Creator: creator,
				PostId:  postID,
				Body:    string(bytes.Repeat([]byte("a"), 2001)),
			},
			expectError: true,
			errContains: "body exceeds maximum length",
		},
		{
			name: "parent reply not found",
			msg: &types.MsgCreateReply{
				Creator:       creator,
				PostId:        postID,
				ParentReplyId: 99999,
				Body:          "Reply to nonexistent parent",
			},
			expectError: true,
			errContains: "parent reply",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				targetPostID := tt.setup()
				tt.msg.PostId = targetPostID
			}

			resp, err := msgServer.CreateReply(ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)

				// Verify reply was created
				reply, found := k.GetReply(ctx, resp.Id)
				require.True(t, found)
				require.Equal(t, tt.msg.Creator, reply.Creator)
				require.Equal(t, tt.msg.PostId, reply.PostId)
				require.Equal(t, tt.msg.Body, reply.Body)
				require.Equal(t, types.ReplyStatus_REPLY_STATUS_ACTIVE, reply.Status)
			}
		})
	}
}

func TestCreateReplyNested(t *testing.T) {
	k, msgServer, ctx, _ := setupMsgServer(t)

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	// Create a post
	postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
		Creator: creator,
		Title:   "Test Post",
		Body:    "Post body",
	})
	require.NoError(t, err)

	// Create a dummy reply first so the next reply gets a non-zero ID.
	// Reply IDs start at 0, and ParentReplyId=0 is treated as "no parent",
	// so the parent reply must have ID >= 1.
	_, err = msgServer.CreateReply(ctx, &types.MsgCreateReply{
		Creator: creator,
		PostId:  postResp.Id,
		Body:    "Dummy reply to consume ID 0",
	})
	require.NoError(t, err)

	// Create the top-level reply (will get ID 1)
	replyResp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
		Creator: creator,
		PostId:  postResp.Id,
		Body:    "Top-level reply",
	})
	require.NoError(t, err)
	topReplyID := replyResp.Id
	require.NotZero(t, topReplyID, "top reply ID must be > 0 to be used as ParentReplyId")

	// Create a nested reply (reply to the top-level reply)
	nestedResp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
		Creator:       creator,
		PostId:        postResp.Id,
		ParentReplyId: topReplyID,
		Body:          "Nested reply",
	})
	require.NoError(t, err)

	// Verify nested reply
	nestedReply, found := k.GetReply(ctx, nestedResp.Id)
	require.True(t, found)
	require.Equal(t, topReplyID, nestedReply.ParentReplyId)
	require.Equal(t, postResp.Id, nestedReply.PostId)
	require.Equal(t, uint32(1), nestedReply.Depth)
}

func TestCreateReplyParentReplyOnDifferentPost(t *testing.T) {
	k, msgServer, ctx, _ := setupMsgServer(t)

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	// Create two posts
	postResp1, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
		Creator: creator,
		Title:   "Post One",
		Body:    "Body one",
	})
	require.NoError(t, err)

	postResp2, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
		Creator: creator,
		Title:   "Post Two",
		Body:    "Body two",
	})
	require.NoError(t, err)

	// Create a dummy reply on post 1 to consume ID 0 (since ParentReplyId=0 means "no parent")
	_, err = msgServer.CreateReply(ctx, &types.MsgCreateReply{
		Creator: creator,
		PostId:  postResp1.Id,
		Body:    "Dummy reply to consume ID 0",
	})
	require.NoError(t, err)

	// Create the reply on post 1 that we'll reference (will get ID >= 1)
	replyResp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
		Creator: creator,
		PostId:  postResp1.Id,
		Body:    "Reply on post 1",
	})
	require.NoError(t, err)
	require.NotZero(t, replyResp.Id, "reply ID must be > 0 to be used as ParentReplyId")

	// Try to create a nested reply on post 2, referencing the reply on post 1
	_, err = msgServer.CreateReply(ctx, &types.MsgCreateReply{
		Creator:       creator,
		PostId:        postResp2.Id,
		ParentReplyId: replyResp.Id,
		Body:          "Cross-post nested reply",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "parent reply belongs to a different post")

	// Verify post 2 reply count unchanged
	post2, found := k.GetPost(ctx, postResp2.Id)
	require.True(t, found)
	require.Equal(t, uint64(0), post2.ReplyCount)
}

func TestCreateReplyIncrementsReplyCount(t *testing.T) {
	k, msgServer, ctx, _ := setupMsgServer(t)

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	// Create a post
	postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
		Creator: creator,
		Title:   "Test Post",
		Body:    "Post body",
	})
	require.NoError(t, err)

	// Verify initial reply count is 0
	post, found := k.GetPost(ctx, postResp.Id)
	require.True(t, found)
	require.Equal(t, uint64(0), post.ReplyCount)

	// Create 3 replies
	for i := 0; i < 3; i++ {
		_, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
			Creator: creator,
			PostId:  postResp.Id,
			Body:    "Reply body",
		})
		require.NoError(t, err)
	}

	// Verify reply count is now 3
	post, found = k.GetPost(ctx, postResp.Id)
	require.True(t, found)
	require.Equal(t, uint64(3), post.ReplyCount)
}
