package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func TestUserReaction(t *testing.T) {
	creator1 := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	creator2 := "sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y"

	tests := []struct {
		name        string
		setup       func(t *testing.T, msgServer types.MsgServer, ctx sdk.Context) uint64
		req         func(postId uint64) *types.QueryUserReactionRequest
		expectError bool
		errContains string
		validate    func(t *testing.T, resp *types.QueryUserReactionResponse)
	}{
		{
			name:        "nil request",
			req:         func(_ uint64) *types.QueryUserReactionRequest { return nil },
			expectError: true,
			errContains: "invalid request",
		},
		{
			name: "no reaction by user returns nil",
			setup: func(t *testing.T, msgServer types.MsgServer, ctx sdk.Context) uint64 {
				resp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
					Creator: creator1,
					Title:   "Test Post",
					Body:    "Test body",
				})
				require.NoError(t, err)
				return resp.Id
			},
			req: func(postId uint64) *types.QueryUserReactionRequest {
				return &types.QueryUserReactionRequest{
					Creator: creator2,
					PostId:  postId,
					ReplyId: 0,
				}
			},
			validate: func(t *testing.T, resp *types.QueryUserReactionResponse) {
				require.Nil(t, resp.Reaction)
			},
		},
		{
			name: "returns reaction after user reacts",
			setup: func(t *testing.T, msgServer types.MsgServer, ctx sdk.Context) uint64 {
				resp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
					Creator: creator1,
					Title:   "Reaction Target",
					Body:    "Body for reaction",
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

				return postId
			},
			req: func(postId uint64) *types.QueryUserReactionRequest {
				return &types.QueryUserReactionRequest{
					Creator: creator1,
					PostId:  postId,
					ReplyId: 0,
				}
			},
			validate: func(t *testing.T, resp *types.QueryUserReactionResponse) {
				require.NotNil(t, resp.Reaction)
				require.Equal(t, creator1, resp.Reaction.Creator)
				require.Equal(t, types.ReactionType_REACTION_TYPE_LIKE, resp.Reaction.ReactionType)
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
			resp, err := queryServer.UserReaction(ctx, req)

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
