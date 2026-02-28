package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func TestQueryListExpiringContent(t *testing.T) {
	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	tests := []struct {
		name        string
		setup       func(k keeper.Keeper, msgServer types.MsgServer, ctx sdk.Context)
		req         *types.QueryListExpiringContentRequest
		expectError bool
		checkResp   func(t *testing.T, resp *types.QueryListExpiringContentResponse)
	}{
		{
			name:        "nil request",
			req:         nil,
			expectError: true,
		},
		{
			name: "no expiring content",
			req:  &types.QueryListExpiringContentRequest{ExpiresBefore: 999999},
			checkResp: func(t *testing.T, resp *types.QueryListExpiringContentResponse) {
				require.Empty(t, resp.Posts)
				require.Empty(t, resp.Replies)
			},
		},
		{
			name: "after adding to expiry index and creating content",
			setup: func(k keeper.Keeper, msgServer types.MsgServer, ctx sdk.Context) {
				// Create a post via msgServer
				postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
					Creator: creator,
					Title:   "Expiring Post",
					Body:    "This post will expire",
				})
				require.NoError(t, err)

				// Create a reply via msgServer
				replyResp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
					Creator: creator,
					PostId:  postResp.Id,
					Body:    "This reply will expire",
				})
				require.NoError(t, err)

				// Add both to expiry index
				k.AddToExpiryIndex(ctx, 5000, "post", postResp.Id)
				k.AddToExpiryIndex(ctx, 6000, "reply", replyResp.Id)
			},
			req: &types.QueryListExpiringContentRequest{ExpiresBefore: 10000},
			checkResp: func(t *testing.T, resp *types.QueryListExpiringContentResponse) {
				require.Len(t, resp.Posts, 1)
				require.Equal(t, "Expiring Post", resp.Posts[0].Title)
				require.Len(t, resp.Replies, 1)
				require.Equal(t, "This reply will expire", resp.Replies[0].Body)
			},
		},
		{
			name: "content_type filter post only",
			setup: func(k keeper.Keeper, msgServer types.MsgServer, ctx sdk.Context) {
				// Create a post and a reply
				postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
					Creator: creator,
					Title:   "Filtered Post",
					Body:    "Only this should appear",
				})
				require.NoError(t, err)

				replyResp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
					Creator: creator,
					PostId:  postResp.Id,
					Body:    "This reply should be filtered out",
				})
				require.NoError(t, err)

				k.AddToExpiryIndex(ctx, 3000, "post", postResp.Id)
				k.AddToExpiryIndex(ctx, 3500, "reply", replyResp.Id)
			},
			req: &types.QueryListExpiringContentRequest{
				ExpiresBefore: 10000,
				ContentType:   "post",
			},
			checkResp: func(t *testing.T, resp *types.QueryListExpiringContentResponse) {
				require.Len(t, resp.Posts, 1)
				require.Equal(t, "Filtered Post", resp.Posts[0].Title)
				require.Empty(t, resp.Replies)
			},
		},
		{
			name: "content_type filter reply only",
			setup: func(k keeper.Keeper, msgServer types.MsgServer, ctx sdk.Context) {
				// Create a post and a reply
				postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
					Creator: creator,
					Title:   "Post for reply filter test",
					Body:    "This post should be filtered out",
				})
				require.NoError(t, err)

				replyResp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
					Creator: creator,
					PostId:  postResp.Id,
					Body:    "Only this reply should appear",
				})
				require.NoError(t, err)

				k.AddToExpiryIndex(ctx, 4000, "post", postResp.Id)
				k.AddToExpiryIndex(ctx, 4500, "reply", replyResp.Id)
			},
			req: &types.QueryListExpiringContentRequest{
				ExpiresBefore: 10000,
				ContentType:   "reply",
			},
			checkResp: func(t *testing.T, resp *types.QueryListExpiringContentResponse) {
				require.Empty(t, resp.Posts)
				require.Len(t, resp.Replies, 1)
				require.Equal(t, "Only this reply should appear", resp.Replies[0].Body)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, msgServer, ctx, _ := setupMsgServer(t)
			queryServer := keeper.NewQueryServerImpl(k)

			if tt.setup != nil {
				tt.setup(k, msgServer, ctx)
			}

			resp, err := queryServer.ListExpiringContent(ctx, tt.req)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			if tt.checkResp != nil {
				tt.checkResp(t, resp)
			}
		})
	}
}
