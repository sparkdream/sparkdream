package keeper_test

import (
	"context"
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
	reptypes "sparkdream/x/rep/types"
	shieldtypes "sparkdream/x/shield/types"
)

// Regression: when EndBlock tombstones an ephemeral post that carried tags,
// it must decrement UsageCount for each tag in the rep registry. Cousin of
// the MsgDeletePost decrement fix — both paths walk through the tombstone
// branch in endblock.go and need symmetric rep bookkeeping. Without this,
// ephemeral-post churn silently inflates UsageCount and ExpireTags stops
// reclaiming idle tags.
func TestEndBlockTombstoneDecrementsTagUsage(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	f.repKeeper.IsActiveMemberFn = func(_ context.Context, _ sdk.AccAddress) bool { return false }
	f.repKeeper.KnownTags = map[string]bool{"x": true, "y": true}

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxPostsPerDay = 100
	params.CostPerByteExempt = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	baseTime := int64(2_000_000)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	f.ctx = sdkCtx.WithBlockTime(time.Unix(baseTime, 0))

	resp, err := msgServer.CreatePost(f.ctx, &types.MsgCreatePost{
		Creator: "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan",
		Title:   "Tagged Ephemeral",
		Body:    "tombstone-bound",
		Tags:    []string{"x", "y"},
	})
	require.NoError(t, err)
	// Creates incremented usage for x and y (sanity check before delete path).
	require.Len(t, f.repKeeper.IncrementTagUsageCalls, 2)
	require.Empty(t, f.repKeeper.DecrementTagUsageCalls)

	postID := resp.Id
	expiresAt := baseTime + 100
	post, found := f.keeper.GetPost(f.ctx, postID)
	require.True(t, found)
	post.ExpiresAt = expiresAt
	f.keeper.SetPost(f.ctx, post)
	f.keeper.AddToExpiryIndex(f.ctx, expiresAt, "post", postID)

	sdkCtx = sdk.UnwrapSDKContext(f.ctx)
	f.ctx = sdkCtx.WithBlockTime(time.Unix(expiresAt+1, 0))
	require.NoError(t, f.keeper.EndBlock(f.ctx))

	// Post tombstoned and tags decremented exactly once each.
	post, found = f.keeper.GetPost(f.ctx, postID)
	require.True(t, found)
	require.Equal(t, types.PostStatus_POST_STATUS_DELETED, post.Status)
	require.Nil(t, post.Tags, "tags must be cleared on tombstone")
	require.ElementsMatch(t, []string{"x", "y"}, f.repKeeper.DecrementTagUsageCalls,
		"each tag on the tombstoned post must be decremented exactly once")
}

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

// Regression: anonymous (shield-routed) posts must be ephemeral, renew while
// conviction holds, and tombstone once it drops. They used to be created
// permanent because the shield account passes the membership gate, and the
// renewal branch compared against the blog module account instead.
func TestEndBlockAnonymousPostConvictionLifecycle(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	shieldAddr := authtypes.NewModuleAddress("shield").String()

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxPostsPerDay = 100
	params.CostPerByteExempt = true
	params.ConvictionRenewalThreshold = math.LegacyNewDec(10)
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	baseTime := int64(3_000_000)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockTime(time.Unix(baseTime, 0))

	resp, err := msgServer.CreatePost(f.ctx, &types.MsgCreatePost{
		Creator: shieldAddr,
		Title:   "Anonymous",
		Body:    "posted through x/shield",
	})
	require.NoError(t, err)

	post, found := f.keeper.GetPost(f.ctx, resp.Id)
	require.True(t, found)
	require.Equal(t, baseTime+params.EphemeralContentTtl, post.ExpiresAt, "anonymous posts must be ephemeral")

	// Conviction above the threshold at expiry: sustained and renewed.
	conviction := math.LegacyNewDec(10)
	f.repKeeper.GetContentConvictionFn = func(context.Context, reptypes.StakeTargetType, uint64) (math.LegacyDec, error) {
		return conviction, nil
	}
	expiry := post.ExpiresAt
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockTime(time.Unix(expiry+1, 0))
	require.NoError(t, f.keeper.EndBlock(f.ctx))

	post, _ = f.keeper.GetPost(f.ctx, resp.Id)
	require.NotEqual(t, types.PostStatus_POST_STATUS_DELETED, post.Status)
	require.True(t, post.ConvictionSustained)
	require.Equal(t, expiry+1+params.ConvictionRenewalPeriod, post.ExpiresAt)

	// Conviction gone at the next expiry: tombstoned.
	conviction = math.LegacyZeroDec()
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockTime(time.Unix(post.ExpiresAt+1, 0))
	require.NoError(t, f.keeper.EndBlock(f.ctx))

	post, _ = f.keeper.GetPost(f.ctx, resp.Id)
	require.Equal(t, types.PostStatus_POST_STATUS_DELETED, post.Status)
}

// BLG-02: reply ids and post ids are separate sequences, so an anonymous
// reply's conviction renewal must read STAKE_TARGET_BLOG_REPLY_CONTENT.
// Conviction staked on the post that shares the reply's id must not keep the
// reply alive; conviction on the reply itself does.
func TestEndBlockAnonymousReplyConvictionUsesReplyTarget(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	shieldAddr := authtypes.NewModuleAddress("shield").String()

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxPostsPerDay = 100
	params.MaxRepliesPerDay = 100
	params.CostPerByteExempt = true
	params.ConvictionRenewalThreshold = math.LegacyNewDec(10)
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockTime(time.Unix(4_000_000, 0))

	postResp, err := msgServer.CreatePost(f.ctx, &types.MsgCreatePost{
		Creator: "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan", Title: "Open", Body: "replies welcome", MinReplyTrustLevel: -1,
	})
	require.NoError(t, err)
	replyResp, err := msgServer.CreateReply(shieldtypes.WithProvenTrustLevel(sdk.UnwrapSDKContext(f.ctx), 1), &types.MsgCreateReply{
		Creator: shieldAddr, PostId: postResp.Id, Body: "anonymous reply",
	})
	require.NoError(t, err)
	replyID := replyResp.Id
	reply, found := f.keeper.GetReply(f.ctx, replyID)
	require.True(t, found)
	require.NotZero(t, reply.ExpiresAt, "anonymous replies are ephemeral")

	staked := map[reptypes.StakeTargetType]uint64{}
	f.repKeeper.GetContentConvictionFn = func(_ context.Context, tt reptypes.StakeTargetType, id uint64) (math.LegacyDec, error) {
		if target, ok := staked[tt]; ok && target == id {
			return math.LegacyNewDec(50), nil
		}
		return math.LegacyZeroDec(), nil
	}

	// Conviction on the post with the reply's id: not the reply's.
	staked[reptypes.StakeTargetType_STAKE_TARGET_BLOG_CONTENT] = replyID
	expiry := reply.ExpiresAt
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockTime(time.Unix(expiry+1, 0))

	// Run on a branch first: with only post conviction the reply dies.
	branch, _ := sdk.UnwrapSDKContext(f.ctx).CacheContext()
	require.NoError(t, f.keeper.EndBlock(branch))
	got, _ := f.keeper.GetReply(branch, replyID)
	require.Equal(t, types.ReplyStatus_REPLY_STATUS_DELETED, got.Status, "post conviction must not renew a reply")

	// Conviction on the reply itself renews it.
	staked[reptypes.StakeTargetType_STAKE_TARGET_BLOG_REPLY_CONTENT] = replyID
	require.NoError(t, f.keeper.EndBlock(f.ctx))
	got, _ = f.keeper.GetReply(f.ctx, replyID)
	require.NotEqual(t, types.ReplyStatus_REPLY_STATUS_DELETED, got.Status)
	require.True(t, got.ConvictionSustained)
}

// BLG-03: the expiry pass handles at most MaxExpiriesPerBlock entries per
// block, oldest first; the rest wait for the next blocks.
func TestEndBlockExpiryPassIsCapped(t *testing.T) {
	f := initFixture(t)
	old := keeper.MaxExpiriesPerBlock
	keeper.MaxExpiriesPerBlock = 3
	defer func() { keeper.MaxExpiriesPerBlock = old }()

	msgServer := keeper.NewMsgServerImpl(f.keeper)
	f.repKeeper.IsActiveMemberFn = func(_ context.Context, _ sdk.AccAddress) bool { return false }
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxPostsPerDay = 100
	params.CostPerByteExempt = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockTime(time.Unix(5_000_000, 0))

	var ids []uint64
	for i := 0; i < 5; i++ {
		resp, err := msgServer.CreatePost(f.ctx, &types.MsgCreatePost{Creator: "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan", Title: "Ephemeral", Body: "expires"})
		require.NoError(t, err)
		ids = append(ids, resp.Id)
	}
	post, _ := f.keeper.GetPost(f.ctx, ids[0])
	require.NotZero(t, post.ExpiresAt)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockTime(time.Unix(post.ExpiresAt+1, 0))

	deleted := func() int {
		n := 0
		for _, id := range ids {
			p, _ := f.keeper.GetPost(f.ctx, id)
			if p.Status == types.PostStatus_POST_STATUS_DELETED {
				n++
			}
		}
		return n
	}
	require.NoError(t, f.keeper.EndBlock(f.ctx))
	require.Equal(t, 3, deleted())
	require.NoError(t, f.keeper.EndBlock(f.ctx))
	require.Equal(t, 5, deleted())
}
