package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func TestListReactions(t *testing.T) {
	creator1 := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	creator2 := "sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y"

	tests := []struct {
		name        string
		setup       func(t *testing.T, msgServer types.MsgServer, ctx sdk.Context) uint64
		req         func(postId uint64) *types.QueryListReactionsRequest
		expectError bool
		errContains string
		validate    func(t *testing.T, resp *types.QueryListReactionsResponse)
	}{
		{
			name:        "nil request",
			req:         func(_ uint64) *types.QueryListReactionsRequest { return nil },
			expectError: true,
			errContains: "invalid request",
		},
		{
			name: "no reactions for target returns empty",
			setup: func(t *testing.T, msgServer types.MsgServer, ctx sdk.Context) uint64 {
				resp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
					Creator: creator1,
					Title:   "No Reactions Post",
					Body:    "No one reacted",
				})
				require.NoError(t, err)
				return resp.Id
			},
			req: func(postId uint64) *types.QueryListReactionsRequest {
				return &types.QueryListReactionsRequest{
					PostId:  postId,
					ReplyId: 0,
				}
			},
			validate: func(t *testing.T, resp *types.QueryListReactionsResponse) {
				require.Empty(t, resp.Reactions)
			},
		},
		{
			name: "multiple reactions on same post",
			setup: func(t *testing.T, msgServer types.MsgServer, ctx sdk.Context) uint64 {
				resp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
					Creator: creator1,
					Title:   "Popular Post",
					Body:    "Everyone reacts",
				})
				require.NoError(t, err)
				postId := resp.Id

				// creator1 likes the post
				_, err = msgServer.React(ctx, &types.MsgReact{
					Creator:      creator1,
					PostId:       postId,
					ReplyId:      0,
					ReactionType: types.ReactionType_REACTION_TYPE_LIKE,
				})
				require.NoError(t, err)

				// creator2 finds it funny
				_, err = msgServer.React(ctx, &types.MsgReact{
					Creator:      creator2,
					PostId:       postId,
					ReplyId:      0,
					ReactionType: types.ReactionType_REACTION_TYPE_FUNNY,
				})
				require.NoError(t, err)

				return postId
			},
			req: func(postId uint64) *types.QueryListReactionsRequest {
				return &types.QueryListReactionsRequest{
					PostId:  postId,
					ReplyId: 0,
				}
			},
			validate: func(t *testing.T, resp *types.QueryListReactionsResponse) {
				require.Len(t, resp.Reactions, 2)

				// Build a map of creator -> reaction type for order-independent checking
				reactionMap := make(map[string]types.ReactionType)
				for _, r := range resp.Reactions {
					reactionMap[r.Creator] = r.ReactionType
				}
				require.Equal(t, types.ReactionType_REACTION_TYPE_LIKE, reactionMap[creator1])
				require.Equal(t, types.ReactionType_REACTION_TYPE_FUNNY, reactionMap[creator2])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, msgServer, ctx, _ := setupMsgServer(t)
			queryServer := keeper.NewQueryServerImpl(k)

			var postId uint64
			if tt.setup != nil {
				postId = tt.setup(t, msgServer, ctx)
			}

			req := tt.req(postId)
			resp, err := queryServer.ListReactions(ctx, req)

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
