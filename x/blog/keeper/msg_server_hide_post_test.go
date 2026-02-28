package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/types"
)

func TestHidePost(t *testing.T) {
	k, msgServer, ctx, _ := setupMsgServer(t)

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	otherAddr := "sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y"

	// Create a post for the happy path test
	createResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
		Creator: creator,
		Title:   "Test",
		Body:    "Body",
	})
	require.NoError(t, err)
	activePostID := createResp.Id

	// Create a post, then delete it to test hiding a non-active post
	deleteResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
		Creator: creator,
		Title:   "Delete Me",
		Body:    "Will be deleted",
	})
	require.NoError(t, err)
	deletedPostID := deleteResp.Id
	_, err = msgServer.DeletePost(ctx, &types.MsgDeletePost{Creator: creator, Id: deletedPostID})
	require.NoError(t, err)

	// Create a post owned by creator for the unauthorized test
	unauthResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
		Creator: creator,
		Title:   "Auth Test",
		Body:    "Auth body",
	})
	require.NoError(t, err)
	unauthPostID := unauthResp.Id

	tests := []struct {
		name        string
		msg         *types.MsgHidePost
		expectError bool
		errContains string
	}{
		{
			name: "successful hide by post author",
			msg: &types.MsgHidePost{
				Creator: creator,
				Id:      activePostID,
			},
			expectError: false,
		},
		{
			name: "post not found",
			msg: &types.MsgHidePost{
				Creator: creator,
				Id:      99999,
			},
			expectError: true,
			errContains: "doesn't exist",
		},
		{
			name: "post not active (already deleted)",
			msg: &types.MsgHidePost{
				Creator: creator,
				Id:      deletedPostID,
			},
			expectError: true,
			errContains: "has been deleted",
		},
		{
			name: "not the post author (unauthorized)",
			msg: &types.MsgHidePost{
				Creator: otherAddr,
				Id:      unauthPostID,
			},
			expectError: true,
			errContains: "only post author can hide a post",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := msgServer.HidePost(ctx, tt.msg)
			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}

	// Verify post status is HIDDEN and HiddenBy set after successful hide
	t.Run("verify post status is HIDDEN and HiddenBy set", func(t *testing.T) {
		post, found := k.GetPost(ctx, activePostID)
		require.True(t, found)
		require.Equal(t, types.PostStatus_POST_STATUS_HIDDEN, post.Status)
		require.Equal(t, creator, post.HiddenBy)
		require.NotZero(t, post.HiddenAt)
	})
}
