package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func TestListPostsByCreator(t *testing.T) {
	creator1 := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	creator2 := "sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y"

	tests := []struct {
		name        string
		setup       func(t *testing.T, k keeper.Keeper, msgServer types.MsgServer, ctx sdk.Context)
		req         *types.QueryListPostsByCreatorRequest
		expectError bool
		errContains string
		validate    func(t *testing.T, resp *types.QueryListPostsByCreatorResponse)
	}{
		{
			name:        "nil request",
			req:         nil,
			expectError: true,
			errContains: "invalid request",
		},
		{
			name: "no posts by creator",
			req: &types.QueryListPostsByCreatorRequest{
				Creator: creator1,
			},
			validate: func(t *testing.T, resp *types.QueryListPostsByCreatorResponse) {
				require.Empty(t, resp.Posts)
			},
		},
		{
			name: "returns only posts by requested creator",
			setup: func(t *testing.T, k keeper.Keeper, msgServer types.MsgServer, ctx sdk.Context) {
				// Create posts by creator1
				for i := 0; i < 3; i++ {
					_, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
						Creator: creator1,
						Title:   "Post by creator1",
						Body:    "Body by creator1",
					})
					require.NoError(t, err)
				}
				// Create posts by creator2
				for i := 0; i < 2; i++ {
					_, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
						Creator: creator2,
						Title:   "Post by creator2",
						Body:    "Body by creator2",
					})
					require.NoError(t, err)
				}
			},
			req: &types.QueryListPostsByCreatorRequest{
				Creator: creator1,
			},
			validate: func(t *testing.T, resp *types.QueryListPostsByCreatorResponse) {
				require.Len(t, resp.Posts, 3)
				for _, post := range resp.Posts {
					require.Equal(t, creator1, post.Creator)
				}
			},
		},
		{
			name: "excludes deleted posts",
			setup: func(t *testing.T, k keeper.Keeper, msgServer types.MsgServer, ctx sdk.Context) {
				// Create two posts by creator1
				resp1, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
					Creator: creator1,
					Title:   "Active Post",
					Body:    "Active body",
				})
				require.NoError(t, err)

				_, err = msgServer.CreatePost(ctx, &types.MsgCreatePost{
					Creator: creator1,
					Title:   "To Delete",
					Body:    "Will be deleted",
				})
				require.NoError(t, err)

				// Delete the second post
				_, err = msgServer.DeletePost(ctx, &types.MsgDeletePost{
					Creator: creator1,
					Id:      resp1.Id + 1,
				})
				require.NoError(t, err)
			},
			req: &types.QueryListPostsByCreatorRequest{
				Creator: creator1,
			},
			validate: func(t *testing.T, resp *types.QueryListPostsByCreatorResponse) {
				require.Len(t, resp.Posts, 1)
				require.Equal(t, "Active Post", resp.Posts[0].Title)
			},
		},
		{
			name: "include_hidden false skips hidden posts",
			setup: func(t *testing.T, k keeper.Keeper, msgServer types.MsgServer, ctx sdk.Context) {
				// Create two posts
				resp1, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
					Creator: creator1,
					Title:   "Visible Post",
					Body:    "Visible body",
				})
				require.NoError(t, err)

				_, err = msgServer.CreatePost(ctx, &types.MsgCreatePost{
					Creator: creator1,
					Title:   "Hidden Post",
					Body:    "Hidden body",
				})
				require.NoError(t, err)

				// Hide the second post
				_, err = msgServer.HidePost(ctx, &types.MsgHidePost{
					Creator: creator1,
					Id:      resp1.Id + 1,
				})
				require.NoError(t, err)
			},
			req: &types.QueryListPostsByCreatorRequest{
				Creator:       creator1,
				IncludeHidden: false,
			},
			validate: func(t *testing.T, resp *types.QueryListPostsByCreatorResponse) {
				require.Len(t, resp.Posts, 1)
				require.Equal(t, "Visible Post", resp.Posts[0].Title)
			},
		},
		{
			name: "include_hidden true includes hidden posts",
			setup: func(t *testing.T, k keeper.Keeper, msgServer types.MsgServer, ctx sdk.Context) {
				// Create two posts
				resp1, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
					Creator: creator1,
					Title:   "Visible Post",
					Body:    "Visible body",
				})
				require.NoError(t, err)

				_, err = msgServer.CreatePost(ctx, &types.MsgCreatePost{
					Creator: creator1,
					Title:   "Hidden Post",
					Body:    "Hidden body",
				})
				require.NoError(t, err)

				// Hide the second post
				_, err = msgServer.HidePost(ctx, &types.MsgHidePost{
					Creator: creator1,
					Id:      resp1.Id + 1,
				})
				require.NoError(t, err)
			},
			req: &types.QueryListPostsByCreatorRequest{
				Creator:       creator1,
				IncludeHidden: true,
			},
			validate: func(t *testing.T, resp *types.QueryListPostsByCreatorResponse) {
				require.Len(t, resp.Posts, 2)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, msgServer, ctx, _ := setupMsgServer(t)
			queryServer := keeper.NewQueryServerImpl(k)

			if tt.setup != nil {
				tt.setup(t, k, msgServer, ctx)
			}

			resp, err := queryServer.ListPostsByCreator(ctx, tt.req)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			if tt.validate != nil {
				tt.validate(t, resp)
			}
		})
	}
}
