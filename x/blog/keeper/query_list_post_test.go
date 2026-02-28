package keeper_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func TestQueryListPost(t *testing.T) {
	k, _, ctx, _ := setupMsgServer(t)
	qs := keeper.NewQueryServerImpl(k)

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	tests := []struct {
		name        string
		setup       func()
		req         *types.QueryListPostRequest
		expectError bool
		errContains string
		check       func(t *testing.T, resp *types.QueryListPostResponse)
	}{
		{
			name:        "nil request",
			req:         nil,
			expectError: true,
			errContains: "invalid request",
		},
		{
			name: "empty store",
			req:  &types.QueryListPostRequest{},
			check: func(t *testing.T, resp *types.QueryListPostResponse) {
				t.Helper()
				require.Empty(t, resp.Post)
			},
		},
		{
			name: "multiple posts, includes tombstoned, excludes hidden",
			setup: func() {
				k.AppendPost(ctx, types.Post{
					Creator: creator,
					Title:   "Active Post",
					Body:    "body",
					Status:  types.PostStatus_POST_STATUS_ACTIVE,
				})
				k.AppendPost(ctx, types.Post{
					Creator: creator,
					Title:   "Deleted Post",
					Body:    "body",
					Status:  types.PostStatus_POST_STATUS_DELETED,
				})
				k.AppendPost(ctx, types.Post{
					Creator: creator,
					Title:   "Hidden Post",
					Body:    "body",
					Status:  types.PostStatus_POST_STATUS_HIDDEN,
				})
				k.AppendPost(ctx, types.Post{
					Creator: creator,
					Title:   "Another Active",
					Body:    "body",
					Status:  types.PostStatus_POST_STATUS_ACTIVE,
				})
			},
			req: &types.QueryListPostRequest{},
			check: func(t *testing.T, resp *types.QueryListPostResponse) {
				t.Helper()
				// Tombstoned posts are included; hidden posts are excluded
				require.Len(t, resp.Post, 3)
				require.Equal(t, "Active Post", resp.Post[0].Title)
				require.Equal(t, "Deleted Post", resp.Post[1].Title)
				require.Equal(t, types.PostStatus_POST_STATUS_DELETED, resp.Post[1].Status)
				require.Equal(t, "Another Active", resp.Post[2].Title)
			},
		},
		{
			name: "pagination works",
			req: &types.QueryListPostRequest{
				Pagination: &query.PageRequest{
					Limit:      1,
					CountTotal: true,
				},
			},
			check: func(t *testing.T, resp *types.QueryListPostResponse) {
				t.Helper()
				// Should return at most 1 post per page.
				require.LessOrEqual(t, len(resp.Post), 1)
				require.NotNil(t, resp.Pagination)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}

			resp, err := qs.ListPost(ctx, tt.req)

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
