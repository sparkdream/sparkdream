package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
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
				// BLG-05: the tombstone stays listed (empty title/body).
				require.Len(t, resp.Posts, 2)
				require.Equal(t, "Active Post", resp.Posts[0].Title)
				require.Equal(t, types.PostStatus_POST_STATUS_DELETED, resp.Posts[1].Status)
				require.Empty(t, resp.Posts[1].Title)
				require.Empty(t, resp.Posts[1].Body)
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

// BLG-08: a migrated author's canonical and pre-migration posts page as one
// stream: limit bounds every page, no post repeats across pages, and every
// post is reached.
func TestListPostsByCreatorPaginatesAliases(t *testing.T) {
	f := initFixture(t)
	canonical := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	old := "sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y"
	f.repKeeper.AllAddressesOfFn = func(a string) []string {
		if a == canonical || a == old {
			return []string{canonical, old}
		}
		return []string{a}
	}
	for i := 0; i < 4; i++ {
		f.keeper.AppendPost(f.ctx, types.Post{Creator: old, Title: "zenith", Body: "b", Status: types.PostStatus_POST_STATUS_ACTIVE})
		f.keeper.AppendPost(f.ctx, types.Post{Creator: canonical, Title: "phoenix", Body: "b", Status: types.PostStatus_POST_STATUS_ACTIVE})
	}
	qs := keeper.NewQueryServerImpl(f.keeper)

	seen := map[uint64]bool{}
	var key []byte
	for page := 0; ; page++ {
		require.Less(t, page, 10)
		resp, err := qs.ListPostsByCreator(f.ctx, &types.QueryListPostsByCreatorRequest{
			Creator:    canonical,
			Pagination: &query.PageRequest{Key: key, Limit: 3},
		})
		require.NoError(t, err)
		require.LessOrEqual(t, len(resp.Posts), 3)
		for _, p := range resp.Posts {
			require.False(t, seen[p.Id], "post %d repeated", p.Id)
			seen[p.Id] = true
		}
		key = resp.Pagination.NextKey
		if len(key) == 0 {
			break
		}
	}
	require.Len(t, seen, 8)

	resp, err := qs.ListPostsByCreator(f.ctx, &types.QueryListPostsByCreatorRequest{
		Creator:    canonical,
		Pagination: &query.PageRequest{Limit: 3, Offset: 6, CountTotal: true},
	})
	require.NoError(t, err)
	require.Len(t, resp.Posts, 2)
	require.Equal(t, uint64(8), resp.Pagination.Total)
}
