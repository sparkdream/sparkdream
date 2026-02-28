package keeper_test

import (
	"context"
	"testing"

	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	module "sparkdream/x/blog/module"
	"sparkdream/x/blog/types"
)

func TestReact(t *testing.T) {
	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	creator2 := "sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y"

	t.Run("successful reaction on post", func(t *testing.T) {
		_, msgServer, ctx, _ := setupMsgServer(t)

		postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Test Post",
			Body:    "Test body",
		})
		require.NoError(t, err)

		resp, err := msgServer.React(ctx, &types.MsgReact{
			Creator:      creator2,
			PostId:       postResp.Id,
			ReplyId:      0,
			ReactionType: types.ReactionType_REACTION_TYPE_LIKE,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
	})

	t.Run("successful reaction on reply", func(t *testing.T) {
		_, msgServer, ctx, _ := setupMsgServer(t)

		postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Test Post",
			Body:    "Test body",
		})
		require.NoError(t, err)

		replyResp, err := msgServer.CreateReply(ctx, &types.MsgCreateReply{
			Creator: creator,
			PostId:  postResp.Id,
			Body:    "Test reply",
		})
		require.NoError(t, err)

		resp, err := msgServer.React(ctx, &types.MsgReact{
			Creator:      creator2,
			PostId:       postResp.Id,
			ReplyId:      replyResp.Id,
			ReactionType: types.ReactionType_REACTION_TYPE_INSIGHTFUL,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
	})

	t.Run("not active member", func(t *testing.T) {
		// Build a keeper with a repKeeper that always returns false.
		encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
		ac := addresscodec.NewBech32Codec("sprkdrm")
		storeKey := storetypes.NewKVStoreKey(types.StoreKey)
		storeService := runtime.NewKVStoreService(storeKey)
		ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx
		authority := authtypes.NewModuleAddress(types.GovModuleName)

		repKeeper := &mockRepKeeper{
			IsActiveMemberFn: func(ctx context.Context, addr sdk.AccAddress) bool {
				return false
			},
		}
		k := keeper.NewKeeper(storeService, encCfg.Codec, ac, authority, &mockBankKeeper{}, nil, repKeeper)
		params := types.DefaultParams()
		params.MaxPostsPerDay = 100
		require.NoError(t, k.Params.Set(ctx, params))
		msgServer := keeper.NewMsgServerImpl(k)

		// Manually store a post so the post-not-found check is not reached first.
		// The isActiveMember check happens before the post lookup, so the post
		// does not strictly need to exist, but we include it for completeness.
		k.SetPost(ctx, types.Post{
			Id:      0,
			Creator: creator,
			Title:   "Test Post",
			Body:    "Test body",
			Status:  types.PostStatus_POST_STATUS_ACTIVE,
		})
		k.SetPostCount(ctx, 1)

		_, err := msgServer.React(ctx, &types.MsgReact{
			Creator:      creator,
			PostId:       0,
			ReplyId:      0,
			ReactionType: types.ReactionType_REACTION_TYPE_LIKE,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not an active member")
	})

	t.Run("unspecified reaction type", func(t *testing.T) {
		_, msgServer, ctx, _ := setupMsgServer(t)

		postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Test Post",
			Body:    "Test body",
		})
		require.NoError(t, err)

		_, err = msgServer.React(ctx, &types.MsgReact{
			Creator:      creator,
			PostId:       postResp.Id,
			ReplyId:      0,
			ReactionType: types.ReactionType_REACTION_TYPE_UNSPECIFIED,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "reaction type must be specified")
	})

	t.Run("post not found", func(t *testing.T) {
		_, msgServer, ctx, _ := setupMsgServer(t)

		_, err := msgServer.React(ctx, &types.MsgReact{
			Creator:      creator,
			PostId:       9999,
			ReplyId:      0,
			ReactionType: types.ReactionType_REACTION_TYPE_LIKE,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "post")
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("post not active", func(t *testing.T) {
		k, msgServer, ctx, _ := setupMsgServer(t)

		postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Test Post",
			Body:    "Test body",
		})
		require.NoError(t, err)

		// Set post to deleted
		post, found := k.GetPost(ctx, postResp.Id)
		require.True(t, found)
		post.Status = types.PostStatus_POST_STATUS_DELETED
		k.SetPost(ctx, post)

		_, err = msgServer.React(ctx, &types.MsgReact{
			Creator:      creator,
			PostId:       postResp.Id,
			ReplyId:      0,
			ReactionType: types.ReactionType_REACTION_TYPE_LIKE,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "has been deleted")
	})

	t.Run("reply not found", func(t *testing.T) {
		_, msgServer, ctx, _ := setupMsgServer(t)

		postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Test Post",
			Body:    "Test body",
		})
		require.NoError(t, err)

		_, err = msgServer.React(ctx, &types.MsgReact{
			Creator:      creator,
			PostId:       postResp.Id,
			ReplyId:      9999,
			ReactionType: types.ReactionType_REACTION_TYPE_LIKE,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "reply")
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("same reaction type is no-op", func(t *testing.T) {
		k, msgServer, ctx, _ := setupMsgServer(t)

		postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Test Post",
			Body:    "Test body",
		})
		require.NoError(t, err)

		// React with LIKE
		_, err = msgServer.React(ctx, &types.MsgReact{
			Creator:      creator2,
			PostId:       postResp.Id,
			ReplyId:      0,
			ReactionType: types.ReactionType_REACTION_TYPE_LIKE,
		})
		require.NoError(t, err)

		// React again with same LIKE - should be no-op
		_, err = msgServer.React(ctx, &types.MsgReact{
			Creator:      creator2,
			PostId:       postResp.Id,
			ReplyId:      0,
			ReactionType: types.ReactionType_REACTION_TYPE_LIKE,
		})
		require.NoError(t, err)

		// Count should still be 1
		counts := k.GetReactionCounts(ctx, postResp.Id, 0)
		require.Equal(t, uint64(1), counts.LikeCount)
	})

	t.Run("different reaction type changes the reaction", func(t *testing.T) {
		k, msgServer, ctx, _ := setupMsgServer(t)

		postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Test Post",
			Body:    "Test body",
		})
		require.NoError(t, err)

		// React with LIKE
		_, err = msgServer.React(ctx, &types.MsgReact{
			Creator:      creator2,
			PostId:       postResp.Id,
			ReplyId:      0,
			ReactionType: types.ReactionType_REACTION_TYPE_LIKE,
		})
		require.NoError(t, err)

		// Change to FUNNY
		_, err = msgServer.React(ctx, &types.MsgReact{
			Creator:      creator2,
			PostId:       postResp.Id,
			ReplyId:      0,
			ReactionType: types.ReactionType_REACTION_TYPE_FUNNY,
		})
		require.NoError(t, err)

		// LIKE count should be 0, FUNNY count should be 1
		counts := k.GetReactionCounts(ctx, postResp.Id, 0)
		require.Equal(t, uint64(0), counts.LikeCount)
		require.Equal(t, uint64(1), counts.FunnyCount)

		// Verify the stored reaction is FUNNY
		reaction, found := k.GetReaction(ctx, postResp.Id, 0, creator2)
		require.True(t, found)
		require.Equal(t, types.ReactionType_REACTION_TYPE_FUNNY, reaction.ReactionType)
	})

	t.Run("verify reaction counts incremented", func(t *testing.T) {
		k, msgServer, ctx, _ := setupMsgServer(t)

		postResp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Test Post",
			Body:    "Test body",
		})
		require.NoError(t, err)

		// Add a LIKE from creator
		_, err = msgServer.React(ctx, &types.MsgReact{
			Creator:      creator,
			PostId:       postResp.Id,
			ReplyId:      0,
			ReactionType: types.ReactionType_REACTION_TYPE_LIKE,
		})
		require.NoError(t, err)

		// Add a LIKE from creator2
		_, err = msgServer.React(ctx, &types.MsgReact{
			Creator:      creator2,
			PostId:       postResp.Id,
			ReplyId:      0,
			ReactionType: types.ReactionType_REACTION_TYPE_LIKE,
		})
		require.NoError(t, err)

		counts := k.GetReactionCounts(ctx, postResp.Id, 0)
		require.Equal(t, uint64(2), counts.LikeCount)
		require.Equal(t, uint64(0), counts.InsightfulCount)
		require.Equal(t, uint64(0), counts.DisagreeCount)
		require.Equal(t, uint64(0), counts.FunnyCount)
	})
}
