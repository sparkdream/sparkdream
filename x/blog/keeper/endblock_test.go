package keeper_test

import (
	"context"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func TestEndBlockExpiresEphemeralPost(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Make creator NOT an active member so content expires instead of upgrading.
	f.repKeeper.IsActiveMemberFn = func(_ context.Context, _ sdk.AccAddress) bool { return false }

	// Raise rate limits so test posts aren't throttled.
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxPostsPerDay = 100
	params.CostPerByteExempt = true // skip fee logic in test
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Set block time so the post creation has a well-defined timestamp.
	baseTime := int64(1_000_000)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	f.ctx = sdkCtx.WithBlockTime(time.Unix(baseTime, 0))

	// Create a post.
	resp, err := msgServer.CreatePost(f.ctx, &types.MsgCreatePost{
		Creator: "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan",
		Title:   "Ephemeral Post",
		Body:    "This post will expire",
	})
	require.NoError(t, err)
	postID := resp.Id

	// Manually set ExpiresAt to a known value and register in expiry index.
	expiresAt := baseTime + 100
	post, found := f.keeper.GetPost(f.ctx, postID)
	require.True(t, found)
	post.ExpiresAt = expiresAt
	f.keeper.SetPost(f.ctx, post)
	f.keeper.AddToExpiryIndex(f.ctx, expiresAt, "post", postID)

	// Advance block time past expiry and run EndBlock.
	sdkCtx = sdk.UnwrapSDKContext(f.ctx)
	f.ctx = sdkCtx.WithBlockTime(time.Unix(expiresAt+1, 0))
	require.NoError(t, f.keeper.EndBlock(f.ctx))

	// Verify the post was tombstoned.
	post, found = f.keeper.GetPost(f.ctx, postID)
	require.True(t, found)
	require.Equal(t, types.PostStatus_POST_STATUS_DELETED, post.Status)
	require.Empty(t, post.Title)
	require.Empty(t, post.Body)
}

func TestEndBlockExpiresEphemeralReply(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Phase 1: creator is active so post is permanent; reply creator is NOT active.
	f.repKeeper.IsActiveMemberFn = func(_ context.Context, _ sdk.AccAddress) bool { return true }

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxPostsPerDay = 100
	params.MaxRepliesPerDay = 100
	params.CostPerByteExempt = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	baseTime := int64(1_000_000)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	f.ctx = sdkCtx.WithBlockTime(time.Unix(baseTime, 0))

	// Create a post (permanent because creator is active).
	postResp, err := msgServer.CreatePost(f.ctx, &types.MsgCreatePost{
		Creator:            "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan",
		Title:              "Parent Post",
		Body:               "Post with reply",
		MinReplyTrustLevel: -1, // allow all repliers
	})
	require.NoError(t, err)
	postID := postResp.Id

	// Create a reply (still permanent because IsActiveMember returns true).
	replyResp, err := msgServer.CreateReply(f.ctx, &types.MsgCreateReply{
		Creator: "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan",
		PostId:  postID,
		Body:    "This reply will expire",
	})
	require.NoError(t, err)
	replyID := replyResp.Id

	// Confirm reply count incremented.
	post, found := f.keeper.GetPost(f.ctx, postID)
	require.True(t, found)
	require.Equal(t, uint64(1), post.ReplyCount)

	// Now switch mock so creator is NOT active (for expiry logic).
	f.repKeeper.IsActiveMemberFn = func(_ context.Context, _ sdk.AccAddress) bool { return false }

	// Manually set ExpiresAt on the reply and register in expiry index.
	expiresAt := baseTime + 200
	reply, found := f.keeper.GetReply(f.ctx, replyID)
	require.True(t, found)
	reply.ExpiresAt = expiresAt
	f.keeper.SetReply(f.ctx, reply)
	f.keeper.AddToExpiryIndex(f.ctx, expiresAt, "reply", replyID)

	// Advance block time past expiry and run EndBlock.
	sdkCtx = sdk.UnwrapSDKContext(f.ctx)
	f.ctx = sdkCtx.WithBlockTime(time.Unix(expiresAt+1, 0))
	require.NoError(t, f.keeper.EndBlock(f.ctx))

	// Verify reply was tombstoned.
	reply, found = f.keeper.GetReply(f.ctx, replyID)
	require.True(t, found)
	require.Equal(t, types.ReplyStatus_REPLY_STATUS_DELETED, reply.Status)
	require.Empty(t, reply.Body)

	// Verify parent post reply count was decremented.
	post, found = f.keeper.GetPost(f.ctx, postID)
	require.True(t, found)
	require.Equal(t, uint64(0), post.ReplyCount)
}

func TestEndBlockUpgradesActiveMemberPost(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Start with creator as non-active so the post is created as ephemeral.
	f.repKeeper.IsActiveMemberFn = func(_ context.Context, _ sdk.AccAddress) bool { return false }

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxPostsPerDay = 100
	params.CostPerByteExempt = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	baseTime := int64(1_000_000)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	f.ctx = sdkCtx.WithBlockTime(time.Unix(baseTime, 0))

	// Create an ephemeral post.
	resp, err := msgServer.CreatePost(f.ctx, &types.MsgCreatePost{
		Creator: "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan",
		Title:   "Will Be Upgraded",
		Body:    "This post will be upgraded to permanent",
	})
	require.NoError(t, err)
	postID := resp.Id

	// Set a concrete expiry and register in index.
	expiresAt := baseTime + 300
	post, found := f.keeper.GetPost(f.ctx, postID)
	require.True(t, found)
	post.ExpiresAt = expiresAt
	f.keeper.SetPost(f.ctx, post)
	f.keeper.AddToExpiryIndex(f.ctx, expiresAt, "post", postID)

	// Now make creator an active member before EndBlock runs.
	f.repKeeper.IsActiveMemberFn = func(_ context.Context, _ sdk.AccAddress) bool { return true }

	// Advance block time past expiry.
	sdkCtx = sdk.UnwrapSDKContext(f.ctx)
	f.ctx = sdkCtx.WithBlockTime(time.Unix(expiresAt+1, 0))
	require.NoError(t, f.keeper.EndBlock(f.ctx))

	// Verify the post was upgraded: ExpiresAt set to 0, status still ACTIVE, content preserved.
	post, found = f.keeper.GetPost(f.ctx, postID)
	require.True(t, found)
	require.Equal(t, int64(0), post.ExpiresAt)
	require.Equal(t, types.PostStatus_POST_STATUS_ACTIVE, post.Status)
	require.Equal(t, "Will Be Upgraded", post.Title)
	require.Equal(t, "This post will be upgraded to permanent", post.Body)
}

func TestEndBlockNoOpWhenNoExpiredContent(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	f.repKeeper.IsActiveMemberFn = func(_ context.Context, _ sdk.AccAddress) bool { return false }

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxPostsPerDay = 100
	params.CostPerByteExempt = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	baseTime := int64(1_000_000)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	f.ctx = sdkCtx.WithBlockTime(time.Unix(baseTime, 0))

	// Create a post.
	resp, err := msgServer.CreatePost(f.ctx, &types.MsgCreatePost{
		Creator: "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan",
		Title:   "Future Expiry",
		Body:    "This post has not expired yet",
	})
	require.NoError(t, err)
	postID := resp.Id

	// Set ExpiresAt far in the future and register in expiry index.
	expiresAt := baseTime + 999999
	post, found := f.keeper.GetPost(f.ctx, postID)
	require.True(t, found)
	post.ExpiresAt = expiresAt
	f.keeper.SetPost(f.ctx, post)
	f.keeper.AddToExpiryIndex(f.ctx, expiresAt, "post", postID)

	// Run EndBlock with current time well before expiry.
	sdkCtx = sdk.UnwrapSDKContext(f.ctx)
	f.ctx = sdkCtx.WithBlockTime(time.Unix(baseTime+100, 0))
	require.NoError(t, f.keeper.EndBlock(f.ctx))

	// Verify the post is completely unchanged.
	post, found = f.keeper.GetPost(f.ctx, postID)
	require.True(t, found)
	require.Equal(t, types.PostStatus_POST_STATUS_ACTIVE, post.Status)
	require.Equal(t, "Future Expiry", post.Title)
	require.Equal(t, "This post has not expired yet", post.Body)
	require.Equal(t, expiresAt, post.ExpiresAt)
}
