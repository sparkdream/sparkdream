package keeper_test

import (
	"testing"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func TestQueryShowPost(t *testing.T) {
	k, _, ctx, _ := setupMsgServer(t)
	qs := keeper.NewQueryServerImpl(k)

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	// Seed a post directly via the keeper.
	postID := k.AppendPost(ctx, types.Post{
		Creator: creator,
		Title:   "First Post",
		Body:    "Body of first post",
		Status:  types.PostStatus_POST_STATUS_ACTIVE,
	})

	tests := []struct {
		name        string
		req         *types.QueryShowPostRequest
		expectError bool
		errContains string
		checkPost   func(t *testing.T, resp *types.QueryShowPostResponse)
	}{
		{
			name:        "nil request",
			req:         nil,
			expectError: true,
			errContains: "invalid request",
		},
		{
			name: "existing post",
			req:  &types.QueryShowPostRequest{Id: postID},
			checkPost: func(t *testing.T, resp *types.QueryShowPostResponse) {
				t.Helper()
				require.Equal(t, postID, resp.Post.Id)
				require.Equal(t, creator, resp.Post.Creator)
				require.Equal(t, "First Post", resp.Post.Title)
				require.Equal(t, "Body of first post", resp.Post.Body)
			},
		},
		{
			name:        "non-existent post",
			req:         &types.QueryShowPostRequest{Id: 999},
			expectError: true,
			errContains: sdkerrors.ErrKeyNotFound.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := qs.ShowPost(ctx, tt.req)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			if tt.checkPost != nil {
				tt.checkPost(t, resp)
			}
		})
	}
}
