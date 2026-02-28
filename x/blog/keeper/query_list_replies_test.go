package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func TestQueryListReplies(t *testing.T) {
	k, _, ctx, _ := setupMsgServer(t)
	qs := keeper.NewQueryServerImpl(k)

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	creator2 := "sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y"

	// Seed two posts.
	postA := k.AppendPost(ctx, types.Post{
		Creator: creator,
		Title:   "Post A",
		Body:    "body",
		Status:  types.PostStatus_POST_STATUS_ACTIVE,
	})
	postB := k.AppendPost(ctx, types.Post{
		Creator: creator,
		Title:   "Post B",
		Body:    "body",
		Status:  types.PostStatus_POST_STATUS_ACTIVE,
	})

	// Reply IDs start at 0 and ParentReplyId=0 means "no parent".
	// Create a dummy reply on postA to consume ID 0, so subsequent
	// replies can use ID 0 as a real parent.
	dummyReplyID := k.AppendReply(ctx, types.Reply{
		PostId:  postA,
		Creator: creator,
		Body:    "dummy reply to consume id 0",
		Status:  types.ReplyStatus_REPLY_STATUS_ACTIVE,
	})

	// Active reply on postA (child of dummy).
	activeReplyID := k.AppendReply(ctx, types.Reply{
		PostId:        postA,
		ParentReplyId: dummyReplyID,
		Creator:       creator,
		Body:          "active reply",
		Status:        types.ReplyStatus_REPLY_STATUS_ACTIVE,
	})

	// Deleted reply on postA.
	k.AppendReply(ctx, types.Reply{
		PostId:  postA,
		Creator: creator,
		Body:    "deleted reply",
		Status:  types.ReplyStatus_REPLY_STATUS_DELETED,
	})

	// Hidden reply on postA.
	hiddenReplyID := k.AppendReply(ctx, types.Reply{
		PostId:  postA,
		Creator: creator2,
		Body:    "hidden reply",
		Status:  types.ReplyStatus_REPLY_STATUS_HIDDEN,
	})

	// Reply on postB (should not appear in postA queries).
	k.AppendReply(ctx, types.Reply{
		PostId:  postB,
		Creator: creator2,
		Body:    "reply on post B",
		Status:  types.ReplyStatus_REPLY_STATUS_ACTIVE,
	})

	tests := []struct {
		name        string
		req         *types.QueryListRepliesRequest
		expectError bool
		errContains string
		check       func(t *testing.T, resp *types.QueryListRepliesResponse)
	}{
		{
			name:        "nil request",
			req:         nil,
			expectError: true,
			errContains: "invalid request",
		},
		{
			name: "empty - no replies for post",
			req:  &types.QueryListRepliesRequest{PostId: 999},
			check: func(t *testing.T, resp *types.QueryListRepliesResponse) {
				t.Helper()
				require.Empty(t, resp.Replies)
			},
		},
		{
			name: "returns replies for specific post, excludes deleted",
			req: &types.QueryListRepliesRequest{
				PostId: postA,
			},
			check: func(t *testing.T, resp *types.QueryListRepliesResponse) {
				t.Helper()
				// postA has: dummy (active), active reply, deleted (excluded), hidden (excluded by default)
				// So we expect dummy + activeReply = 2
				require.Len(t, resp.Replies, 2)
				ids := []uint64{resp.Replies[0].Id, resp.Replies[1].Id}
				require.Contains(t, ids, dummyReplyID)
				require.Contains(t, ids, activeReplyID)
			},
		},
		{
			name: "excludes deleted replies",
			req: &types.QueryListRepliesRequest{
				PostId: postA,
			},
			check: func(t *testing.T, resp *types.QueryListRepliesResponse) {
				t.Helper()
				for _, r := range resp.Replies {
					require.NotEqual(t, types.ReplyStatus_REPLY_STATUS_DELETED, r.Status,
						"deleted replies should not appear")
				}
			},
		},
		{
			name: "include_hidden false skips hidden",
			req: &types.QueryListRepliesRequest{
				PostId:        postA,
				IncludeHidden: false,
			},
			check: func(t *testing.T, resp *types.QueryListRepliesResponse) {
				t.Helper()
				for _, r := range resp.Replies {
					require.NotEqual(t, types.ReplyStatus_REPLY_STATUS_HIDDEN, r.Status,
						"hidden replies should not appear when IncludeHidden is false")
				}
			},
		},
		{
			name: "include_hidden true includes hidden",
			req: &types.QueryListRepliesRequest{
				PostId:        postA,
				IncludeHidden: true,
			},
			check: func(t *testing.T, resp *types.QueryListRepliesResponse) {
				t.Helper()
				// Should return dummy + active + hidden = 3 (deleted still excluded)
				require.Len(t, resp.Replies, 3)
				ids := []uint64{resp.Replies[0].Id, resp.Replies[1].Id, resp.Replies[2].Id}
				require.Contains(t, ids, hiddenReplyID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := qs.ListReplies(ctx, tt.req)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			if tt.check != nil {
				tt.check(t, resp)
			}
		})
	}
}
