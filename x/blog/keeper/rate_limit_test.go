package keeper_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func TestRateLimitAllowsUpToLimit(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Set a low post rate limit.
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxPostsPerDay = 3
	params.CostPerByteExempt = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Set a stable block time so all posts land on the same "day".
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	f.ctx = sdkCtx.WithBlockTime(time.Unix(86400*100, 0)) // day 100

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	// Posts 1 through 3 should succeed.
	for i := 0; i < 3; i++ {
		_, err := msgServer.CreatePost(f.ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Rate Limit Test",
			Body:    "Testing rate limiting",
		})
		require.NoError(t, err, "post %d should succeed", i+1)
	}

	// Post 4 should be rate-limited.
	_, err = msgServer.CreatePost(f.ctx, &types.MsgCreatePost{
		Creator: creator,
		Title:   "Over Limit",
		Body:    "This should fail",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrRateLimitExceeded)
}

func TestRateLimitResetsOnNewDay(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxPostsPerDay = 1
	params.CostPerByteExempt = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	day100Start := int64(86400 * 100)

	// Day 100: first post succeeds.
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	f.ctx = sdkCtx.WithBlockTime(time.Unix(day100Start, 0))

	_, err = msgServer.CreatePost(f.ctx, &types.MsgCreatePost{
		Creator: creator,
		Title:   "Day 100 Post",
		Body:    "First post of the day",
	})
	require.NoError(t, err)

	// Day 100: second post should fail.
	_, err = msgServer.CreatePost(f.ctx, &types.MsgCreatePost{
		Creator: creator,
		Title:   "Day 100 Post 2",
		Body:    "Should be rate limited",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrRateLimitExceeded)

	// Advance to day 101: counter resets.
	sdkCtx = sdk.UnwrapSDKContext(f.ctx)
	f.ctx = sdkCtx.WithBlockTime(time.Unix(day100Start+86400, 0))

	_, err = msgServer.CreatePost(f.ctx, &types.MsgCreatePost{
		Creator: creator,
		Title:   "Day 101 Post",
		Body:    "New day, new allowance",
	})
	require.NoError(t, err)
}

func TestRateLimitZeroMeansNoLimit(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Set MaxPostsPerDay to 0 (unlimited).
	// NOTE: DefaultParams() validates MaxPostsPerDay > 0, so we bypass by
	// setting all fields explicitly to pass validation except for the one we
	// want to test. However, the Params.Validate() call rejects 0, so we
	// test the rate-limit logic directly by setting a high limit and verifying
	// many posts succeed. Alternatively, we can use a value of 0 if the
	// keeper's checkRateLimit short-circuits.
	//
	// checkRateLimit returns nil when limit==0, but Params.Validate rejects 0.
	// We test the keeper-level bypass by directly manipulating the param in the
	// store (Params.Set does NOT call Validate).
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxPostsPerDay = 0
	params.CostPerByteExempt = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	f.ctx = sdkCtx.WithBlockTime(time.Unix(86400*100, 0))

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	// Create many posts; all should succeed when limit is 0.
	for i := 0; i < 20; i++ {
		_, err := msgServer.CreatePost(f.ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Unlimited Post",
			Body:    "No limit enforced",
		})
		require.NoError(t, err, "post %d should succeed with zero limit", i+1)
	}
}

func TestRateLimitPerActionType(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxPostsPerDay = 1
	params.MaxRepliesPerDay = 1
	params.CostPerByteExempt = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	f.ctx = sdkCtx.WithBlockTime(time.Unix(86400*100, 0))

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	// Use up the post limit.
	postResp, err := msgServer.CreatePost(f.ctx, &types.MsgCreatePost{
		Creator:            creator,
		Title:              "Post for Reply",
		Body:               "Body",
		MinReplyTrustLevel: -1, // open replies
	})
	require.NoError(t, err)

	// Second post should fail (limit=1).
	_, err = msgServer.CreatePost(f.ctx, &types.MsgCreatePost{
		Creator: creator,
		Title:   "Excess Post",
		Body:    "Should fail",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrRateLimitExceeded)

	// Reply should still succeed (different action type, limit not yet reached).
	_, err = msgServer.CreateReply(f.ctx, &types.MsgCreateReply{
		Creator: creator,
		PostId:  postResp.Id,
		Body:    "First reply",
	})
	require.NoError(t, err)

	// Second reply should fail.
	_, err = msgServer.CreateReply(f.ctx, &types.MsgCreateReply{
		Creator: creator,
		PostId:  postResp.Id,
		Body:    "Second reply",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrRateLimitExceeded)
}
