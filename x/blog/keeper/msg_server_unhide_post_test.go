package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/types"
)

func TestUnhidePost(t *testing.T) {
	k, msgServer, ctx, _ := setupMsgServer(t)

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	otherAddr := "sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y"

	// Create and hide a post for the happy path test
	createResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
		Creator: creator,
		Title:   "Test",
		Body:    "Body",
	})
	require.NoError(t, err)
	hiddenPostID := createResp.Id
	_, err = msgServer.HidePost(ctx, &types.MsgHidePost{Creator: creator, Id: hiddenPostID})
	require.NoError(t, err)

	// Create an active (non-hidden) post for the "not hidden" test case
	activeResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
		Creator: creator,
		Title:   "Active Post",
		Body:    "Still active",
	})
	require.NoError(t, err)
	activePostID := activeResp.Id

	// Create and hide a post owned by creator for the unauthorized test
	unauthResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
		Creator: creator,
		Title:   "Auth Test",
		Body:    "Auth body",
	})
	require.NoError(t, err)
	unauthPostID := unauthResp.Id
	_, err = msgServer.HidePost(ctx, &types.MsgHidePost{Creator: creator, Id: unauthPostID})
	require.NoError(t, err)

	tests := []struct {
		name        string
		msg         *types.MsgUnhidePost
		expectError bool
		errContains string
	}{
		{
			name: "successful unhide by post author",
			msg: &types.MsgUnhidePost{
				Creator: creator,
				Id:      hiddenPostID,
			},
			expectError: false,
		},
		{
			name: "post not found",
			msg: &types.MsgUnhidePost{
				Creator: creator,
				Id:      99999,
			},
			expectError: true,
			errContains: "doesn't exist",
		},
		{
			name: "post is not hidden (active post)",
			msg: &types.MsgUnhidePost{
				Creator: creator,
				Id:      activePostID,
			},
			expectError: true,
			errContains: "post is not hidden",
		},
		{
			name: "not the post author (unauthorized)",
			msg: &types.MsgUnhidePost{
				Creator: otherAddr,
				Id:      unauthPostID,
			},
			expectError: true,
			errContains: "only post author can unhide a post",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := msgServer.UnhidePost(ctx, tt.msg)
			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}

	// Verify post status is ACTIVE and HiddenBy cleared after successful unhide
	t.Run("verify post status is ACTIVE and HiddenBy cleared", func(t *testing.T) {
		post, found := k.GetPost(ctx, hiddenPostID)
		require.True(t, found)
		require.Equal(t, types.PostStatus_POST_STATUS_ACTIVE, post.Status)
		require.Empty(t, post.HiddenBy)
		require.Zero(t, post.HiddenAt)
	})
}
