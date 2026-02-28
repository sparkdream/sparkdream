package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func TestQueryShowReply(t *testing.T) {
	k, _, ctx, _ := setupMsgServer(t)
	qs := keeper.NewQueryServerImpl(k)

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	// Seed a post so we have a valid PostId.
	postID := k.AppendPost(ctx, types.Post{
		Creator: creator,
		Title:   "Parent Post",
		Body:    "Body",
		Status:  types.PostStatus_POST_STATUS_ACTIVE,
	})

	// Seed a reply directly via the keeper.
	replyID := k.AppendReply(ctx, types.Reply{
		PostId:  postID,
		Creator: creator,
		Body:    "A test reply",
		Status:  types.ReplyStatus_REPLY_STATUS_ACTIVE,
	})

	tests := []struct {
		name        string
		req         *types.QueryShowReplyRequest
		expectError bool
		errContains string
		checkReply  func(t *testing.T, resp *types.QueryShowReplyResponse)
	}{
		{
			name:        "nil request",
			req:         nil,
			expectError: true,
			errContains: "invalid request",
		},
		{
			name: "existing reply",
			req:  &types.QueryShowReplyRequest{Id: replyID},
			checkReply: func(t *testing.T, resp *types.QueryShowReplyResponse) {
				t.Helper()
				require.Equal(t, replyID, resp.Reply.Id)
				require.Equal(t, postID, resp.Reply.PostId)
				require.Equal(t, creator, resp.Reply.Creator)
				require.Equal(t, "A test reply", resp.Reply.Body)
			},
		},
		{
			name:        "non-existent reply",
			req:         &types.QueryShowReplyRequest{Id: 999},
			expectError: true,
			errContains: "reply not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := qs.ShowReply(ctx, tt.req)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			if tt.checkReply != nil {
				tt.checkReply(t, resp)
			}
		})
	}
}
