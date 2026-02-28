package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func TestQueryListReactionsByCreator(t *testing.T) {
	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	tests := []struct {
		name        string
		setup       func(k keeper.Keeper, msgServer types.MsgServer, ctx sdk.Context)
		req         *types.QueryListReactionsByCreatorRequest
		expectError bool
		checkResp   func(t *testing.T, resp *types.QueryListReactionsByCreatorResponse)
	}{
		{
			name:        "nil request",
			req:         nil,
			expectError: true,
		},
		{
			name: "no reactions by creator",
			req:  &types.QueryListReactionsByCreatorRequest{Creator: creator},
			checkResp: func(t *testing.T, resp *types.QueryListReactionsByCreatorResponse) {
				require.Empty(t, resp.Reactions)
			},
		},
		{
			name: "creator has reactions on multiple posts",
			setup: func(k keeper.Keeper, msgServer types.MsgServer, ctx sdk.Context) {
				// Create two posts to react to
				resp1, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
					Creator: creator,
					Title:   "First Post",
					Body:    "Body of the first post",
				})
				require.NoError(t, err)

				resp2, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
					Creator: creator,
					Title:   "Second Post",
					Body:    "Body of the second post",
				})
				require.NoError(t, err)

				// React to first post with LIKE
				_, err = msgServer.React(ctx, &types.MsgReact{
					Creator:      creator,
					PostId:       resp1.Id,
					ReplyId:      0,
					ReactionType: types.ReactionType_REACTION_TYPE_LIKE,
				})
				require.NoError(t, err)

				// React to second post with INSIGHTFUL
				_, err = msgServer.React(ctx, &types.MsgReact{
					Creator:      creator,
					PostId:       resp2.Id,
					ReplyId:      0,
					ReactionType: types.ReactionType_REACTION_TYPE_INSIGHTFUL,
				})
				require.NoError(t, err)
			},
			req: &types.QueryListReactionsByCreatorRequest{Creator: creator},
			checkResp: func(t *testing.T, resp *types.QueryListReactionsByCreatorResponse) {
				require.Len(t, resp.Reactions, 2)

				// Verify all reactions belong to the creator
				for _, r := range resp.Reactions {
					require.Equal(t, creator, r.Creator)
				}

				// Verify we got both reaction types (order may vary)
				reactionTypes := map[types.ReactionType]bool{}
				for _, r := range resp.Reactions {
					reactionTypes[r.ReactionType] = true
				}
				require.True(t, reactionTypes[types.ReactionType_REACTION_TYPE_LIKE])
				require.True(t, reactionTypes[types.ReactionType_REACTION_TYPE_INSIGHTFUL])
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

			resp, err := queryServer.ListReactionsByCreator(ctx, tt.req)

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
