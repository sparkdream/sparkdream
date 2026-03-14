package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func TestReactionCountsInvariant_NoViolation(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Create a post and add reactions via the keeper
	f.keeper.SetPost(f.ctx, types.Post{
		Id:     0,
		Status: types.PostStatus_POST_STATUS_ACTIVE,
	})
	f.keeper.SetPostCount(f.ctx, 1)

	// Add reactions and counts consistently
	f.keeper.SetReaction(f.ctx, types.Reaction{
		PostId:       0,
		ReplyId:      0,
		Creator:      "creator1",
		ReactionType: types.ReactionType_REACTION_TYPE_LIKE,
	})
	f.keeper.SetReaction(f.ctx, types.Reaction{
		PostId:       0,
		ReplyId:      0,
		Creator:      "creator2",
		ReactionType: types.ReactionType_REACTION_TYPE_LIKE,
	})
	f.keeper.SetReactionCounts(f.ctx, 0, 0, types.ReactionCounts{
		LikeCount: 2,
	})

	invariant := keeper.ReactionCountsInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.False(t, broken, "invariant should not be broken: %s", msg)
}

func TestReactionCountsInvariant_Mismatch(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Create a post
	f.keeper.SetPost(f.ctx, types.Post{
		Id:     0,
		Status: types.PostStatus_POST_STATUS_ACTIVE,
	})
	f.keeper.SetPostCount(f.ctx, 1)

	// Add 1 reaction but set count to 5
	f.keeper.SetReaction(f.ctx, types.Reaction{
		PostId:       0,
		ReplyId:      0,
		Creator:      "creator1",
		ReactionType: types.ReactionType_REACTION_TYPE_LIKE,
	})
	f.keeper.SetReactionCounts(f.ctx, 0, 0, types.ReactionCounts{
		LikeCount: 5, // mismatch: only 1 reaction exists
	})

	invariant := keeper.ReactionCountsInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.True(t, broken, "invariant should detect mismatch")
	require.Contains(t, msg, "reaction count mismatches")
}

func TestReplyCountsInvariant_NoViolation(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	f.keeper.SetPost(f.ctx, types.Post{
		Id:         0,
		Status:     types.PostStatus_POST_STATUS_ACTIVE,
		ReplyCount: 2,
	})
	f.keeper.SetPostCount(f.ctx, 1)

	f.keeper.SetReply(f.ctx, types.Reply{
		Id:     0,
		PostId: 0,
		Status: types.ReplyStatus_REPLY_STATUS_ACTIVE,
	})
	f.keeper.SetReply(f.ctx, types.Reply{
		Id:     1,
		PostId: 0,
		Status: types.ReplyStatus_REPLY_STATUS_ACTIVE,
	})
	f.keeper.SetReplyCount(f.ctx, 2)

	invariant := keeper.ReplyCountsInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.False(t, broken, "invariant should not be broken: %s", msg)
}

func TestReplyCountsInvariant_Mismatch(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Post says 3 replies but only 1 active reply exists
	f.keeper.SetPost(f.ctx, types.Post{
		Id:         0,
		Status:     types.PostStatus_POST_STATUS_ACTIVE,
		ReplyCount: 3,
	})
	f.keeper.SetPostCount(f.ctx, 1)

	f.keeper.SetReply(f.ctx, types.Reply{
		Id:     0,
		PostId: 0,
		Status: types.ReplyStatus_REPLY_STATUS_ACTIVE,
	})
	f.keeper.SetReplyCount(f.ctx, 1)

	invariant := keeper.ReplyCountsInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.True(t, broken, "invariant should detect mismatch")
	require.Contains(t, msg, "reply count mismatches")
}

func TestReplyCountsInvariant_DeletedRepliesNotCounted(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Post has 1 active reply and 1 deleted reply
	f.keeper.SetPost(f.ctx, types.Post{
		Id:         0,
		Status:     types.PostStatus_POST_STATUS_ACTIVE,
		ReplyCount: 1, // only active replies count
	})
	f.keeper.SetPostCount(f.ctx, 1)

	f.keeper.SetReply(f.ctx, types.Reply{
		Id:     0,
		PostId: 0,
		Status: types.ReplyStatus_REPLY_STATUS_ACTIVE,
	})
	f.keeper.SetReply(f.ctx, types.Reply{
		Id:     1,
		PostId: 0,
		Status: types.ReplyStatus_REPLY_STATUS_DELETED,
	})
	f.keeper.SetReplyCount(f.ctx, 2)

	invariant := keeper.ReplyCountsInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.False(t, broken, "deleted replies should not be counted: %s", msg)
}

func TestCounterConsistencyInvariant_NoViolation(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	f.keeper.SetPost(f.ctx, types.Post{Id: 0, Status: types.PostStatus_POST_STATUS_ACTIVE})
	f.keeper.SetPost(f.ctx, types.Post{Id: 1, Status: types.PostStatus_POST_STATUS_ACTIVE})
	f.keeper.SetPostCount(f.ctx, 2)

	f.keeper.SetReply(f.ctx, types.Reply{Id: 0, PostId: 0, Status: types.ReplyStatus_REPLY_STATUS_ACTIVE})
	f.keeper.SetReplyCount(f.ctx, 1)

	invariant := keeper.CounterConsistencyInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.False(t, broken, "invariant should not be broken: %s", msg)
}

func TestCounterConsistencyInvariant_PostIDExceedsCount(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Post ID 5 exists but PostCount is only 3
	f.keeper.SetPost(f.ctx, types.Post{Id: 5, Status: types.PostStatus_POST_STATUS_ACTIVE})
	f.keeper.SetPostCount(f.ctx, 3)

	invariant := keeper.CounterConsistencyInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.True(t, broken, "invariant should detect post ID >= PostCount")
	require.Contains(t, msg, "post ID 5 >= PostCount 3")
}

func TestCounterConsistencyInvariant_ReplyIDExceedsCount(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	f.keeper.SetPostCount(f.ctx, 0)

	// Reply ID 10 exists but ReplyCount is only 5
	f.keeper.SetReply(f.ctx, types.Reply{Id: 10, PostId: 0, Status: types.ReplyStatus_REPLY_STATUS_ACTIVE})
	f.keeper.SetReplyCount(f.ctx, 5)

	invariant := keeper.CounterConsistencyInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.True(t, broken, "invariant should detect reply ID >= ReplyCount")
	require.Contains(t, msg, "reply ID 10 >= ReplyCount 5")
}

func TestExpiryIndexInvariant_NoViolation(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Post with expiry (not pinned) - no violation
	f.keeper.SetPost(f.ctx, types.Post{
		Id:        0,
		Status:    types.PostStatus_POST_STATUS_ACTIVE,
		ExpiresAt: 1000,
	})
	f.keeper.SetPostCount(f.ctx, 1)

	invariant := keeper.ExpiryIndexInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.False(t, broken, "invariant should not be broken: %s", msg)
}

func TestExpiryIndexInvariant_PinnedWithExpiry(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Pinned post should have expires_at == 0
	f.keeper.SetPost(f.ctx, types.Post{
		Id:        0,
		Status:    types.PostStatus_POST_STATUS_ACTIVE,
		PinnedBy:  "admin",
		ExpiresAt: 1000, // violation: pinned but has expiry
	})
	f.keeper.SetPostCount(f.ctx, 1)

	invariant := keeper.ExpiryIndexInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.True(t, broken, "invariant should detect pinned post with expiry")
	require.Contains(t, msg, "pinned but has expires_at")
}

func TestExpiryIndexInvariant_PinnedReplyWithExpiry(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	f.keeper.SetReply(f.ctx, types.Reply{
		Id:        0,
		PostId:    0,
		Status:    types.ReplyStatus_REPLY_STATUS_ACTIVE,
		PinnedBy:  "admin",
		ExpiresAt: 500, // violation
	})
	f.keeper.SetReplyCount(f.ctx, 1)
	f.keeper.SetPostCount(f.ctx, 0)

	invariant := keeper.ExpiryIndexInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.True(t, broken, "invariant should detect pinned reply with expiry")
	require.Contains(t, msg, "pinned but has expires_at")
}

func TestHighWaterMarkInvariant_NoViolation(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	f.keeper.SetPost(f.ctx, types.Post{
		Id:                0,
		Title:             "Hello",
		Body:              "World",
		Status:            types.PostStatus_POST_STATUS_ACTIVE,
		FeeBytesHighWater: 100, // >= len("Hello") + len("World") = 10
	})
	f.keeper.SetPostCount(f.ctx, 1)

	invariant := keeper.HighWaterMarkInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.False(t, broken, "invariant should not be broken: %s", msg)
}

func TestHighWaterMarkInvariant_PostViolation(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	f.keeper.SetPost(f.ctx, types.Post{
		Id:                0,
		Title:             "Hello",
		Body:              "World",
		Status:            types.PostStatus_POST_STATUS_ACTIVE,
		FeeBytesHighWater: 5, // < len("Hello") + len("World") = 10
	})
	f.keeper.SetPostCount(f.ctx, 1)

	invariant := keeper.HighWaterMarkInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.True(t, broken, "invariant should detect high water mark violation")
	require.Contains(t, msg, "fee_bytes_high_water=5")
}

func TestHighWaterMarkInvariant_ReplyViolation(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	f.keeper.SetPostCount(f.ctx, 0)
	f.keeper.SetReply(f.ctx, types.Reply{
		Id:                0,
		PostId:            0,
		Body:              "This is a reply body",
		Status:            types.ReplyStatus_REPLY_STATUS_ACTIVE,
		FeeBytesHighWater: 5, // < len("This is a reply body") = 20
	})
	f.keeper.SetReplyCount(f.ctx, 1)

	invariant := keeper.HighWaterMarkInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.True(t, broken, "invariant should detect reply high water mark violation")
	require.Contains(t, msg, "fee_bytes_high_water=5")
}

func TestHighWaterMarkInvariant_DeletedPostSkipped(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Deleted post with low high water - should be skipped
	f.keeper.SetPost(f.ctx, types.Post{
		Id:                0,
		Title:             "Hello",
		Body:              "World",
		Status:            types.PostStatus_POST_STATUS_DELETED,
		FeeBytesHighWater: 0, // normally a violation, but deleted posts are skipped
	})
	f.keeper.SetPostCount(f.ctx, 1)

	invariant := keeper.HighWaterMarkInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.False(t, broken, "deleted posts should be skipped: %s", msg)
}

func TestCounterConsistencyInvariant_EmptyStore(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Empty store - counters at 0
	invariant := keeper.CounterConsistencyInvariant(f.keeper)
	msg, broken := invariant(sdkCtx)
	require.False(t, broken, "empty store should have no violations: %s", msg)
}
