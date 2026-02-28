package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func TestQueryReactionCounts(t *testing.T) {
	creator1 := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	creator2 := "sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y"

	tests := []struct {
		name        string
		setup       func(t *testing.T, msgServer types.MsgServer, ctx sdk.Context) uint64
		req         func(postId uint64) *types.QueryReactionCountsRequest
		expectError bool
		errContains string
		validate    func(t *testing.T, resp *types.QueryReactionCountsResponse)
	}{
		{
			name:        "nil request",
			req:         func(_ uint64) *types.QueryReactionCountsRequest { return nil },
			expectError: true,
			errContains: "invalid request",
		},
		{
			name: "no reactions returns zero counts",
			setup: func(t *testing.T, msgServer types.MsgServer, ctx sdk.Context) uint64 {
				resp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
					Creator: creator1,
					Title:   "Test Post",
					Body:    "Test body",
				})
				require.NoError(t, err)
				return resp.Id
			},
			req: func(postId uint64) *types.QueryReactionCountsRequest {
				return &types.QueryReactionCountsRequest{
					PostId:  postId,
					ReplyId: 0,
				}
			},
			validate: func(t *testing.T, resp *types.QueryReactionCountsResponse) {
				require.Equal(t, uint64(0), resp.Counts.LikeCount)
				require.Equal(t, uint64(0), resp.Counts.InsightfulCount)
				require.Equal(t, uint64(0), resp.Counts.DisagreeCount)
				require.Equal(t, uint64(0), resp.Counts.FunnyCount)
			},
		},
		{
			name: "returns correct counts after reactions",
			setup: func(t *testing.T, msgServer types.MsgServer, ctx sdk.Context) uint64 {
				resp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
					Creator: creator1,
					Title:   "Reactions Post",
					Body:    "Body for reactions",
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

				// creator2 finds it insightful
				_, err = msgServer.React(ctx, &types.MsgReact{
					Creator:      creator2,
					PostId:       postId,
					ReplyId:      0,
					ReactionType: types.ReactionType_REACTION_TYPE_INSIGHTFUL,
				})
				require.NoError(t, err)

				return postId
			},
			req: func(postId uint64) *types.QueryReactionCountsRequest {
				return &types.QueryReactionCountsRequest{
					PostId:  postId,
					ReplyId: 0,
				}
			},
			validate: func(t *testing.T, resp *types.QueryReactionCountsResponse) {
				require.Equal(t, uint64(1), resp.Counts.LikeCount)
				require.Equal(t, uint64(1), resp.Counts.InsightfulCount)
				require.Equal(t, uint64(0), resp.Counts.DisagreeCount)
				require.Equal(t, uint64(0), resp.Counts.FunnyCount)
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
			resp, err := queryServer.ReactionCounts(ctx, req)

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
