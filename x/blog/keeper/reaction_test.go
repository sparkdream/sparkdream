package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/types"
)

func TestSetGetReaction(t *testing.T) {
	f := initFixture(t)
	ctx := f.ctx
	k := f.keeper

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	tests := []struct {
		name     string
		reaction types.Reaction
	}{
		{
			name: "like on post",
			reaction: types.Reaction{
				Creator:      creator,
				ReactionType: types.ReactionType_REACTION_TYPE_LIKE,
				PostId:       1,
				ReplyId:      0,
			},
		},
		{
			name: "insightful on reply",
			reaction: types.Reaction{
				Creator:      creator,
				ReactionType: types.ReactionType_REACTION_TYPE_INSIGHTFUL,
				PostId:       1,
				ReplyId:      5,
			},
		},
		{
			name: "disagree on post",
			reaction: types.Reaction{
				Creator:      creator,
				ReactionType: types.ReactionType_REACTION_TYPE_DISAGREE,
				PostId:       2,
				ReplyId:      0,
			},
		},
		{
			name: "funny on reply",
			reaction: types.Reaction{
				Creator:      creator,
				ReactionType: types.ReactionType_REACTION_TYPE_FUNNY,
				PostId:       3,
				ReplyId:      10,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k.SetReaction(ctx, tt.reaction)

			got, found := k.GetReaction(ctx, tt.reaction.PostId, tt.reaction.ReplyId, tt.reaction.Creator)
			require.True(t, found, "reaction should be found after setting")
			require.Equal(t, tt.reaction.Creator, got.Creator)
			require.Equal(t, tt.reaction.ReactionType, got.ReactionType)
			require.Equal(t, tt.reaction.PostId, got.PostId)
			require.Equal(t, tt.reaction.ReplyId, got.ReplyId)
		})
	}
}

func TestGetReactionNotFound(t *testing.T) {
	f := initFixture(t)
	ctx := f.ctx
	k := f.keeper

	tests := []struct {
		name    string
		postId  uint64
		replyId uint64
		creator string
	}{
		{
			name:    "no reactions exist",
			postId:  1,
			replyId: 0,
			creator: "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan",
		},
		{
			name:    "different post id",
			postId:  999,
			replyId: 0,
			creator: "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan",
		},
		{
			name:    "different reply id",
			postId:  1,
			replyId: 999,
			creator: "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, found := k.GetReaction(ctx, tt.postId, tt.replyId, tt.creator)
			require.False(t, found, "reaction should not be found")
		})
	}
}

func TestRemoveReactionKeeper(t *testing.T) {
	f := initFixture(t)
	ctx := f.ctx
	k := f.keeper

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	reaction := types.Reaction{
		Creator:      creator,
		ReactionType: types.ReactionType_REACTION_TYPE_LIKE,
		PostId:       1,
		ReplyId:      0,
	}

	// Set the reaction
	k.SetReaction(ctx, reaction)

	// Verify it exists
	_, found := k.GetReaction(ctx, 1, 0, creator)
	require.True(t, found, "reaction should exist before removal")

	// Remove it
	k.RemoveReaction(ctx, 1, 0, creator)

	// Verify it no longer exists
	_, found = k.GetReaction(ctx, 1, 0, creator)
	require.False(t, found, "reaction should not be found after removal")
}

func TestRemoveReactionKeeperIdempotent(t *testing.T) {
	f := initFixture(t)
	ctx := f.ctx
	k := f.keeper

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	// Removing a non-existent reaction should not panic
	require.NotPanics(t, func() {
		k.RemoveReaction(ctx, 999, 0, creator)
	})
}

func TestReactionCountsKeeper(t *testing.T) {
	f := initFixture(t)
	ctx := f.ctx
	k := f.keeper

	tests := []struct {
		name    string
		postId  uint64
		replyId uint64
		counts  types.ReactionCounts
	}{
		{
			name:    "counts on a post",
			postId:  1,
			replyId: 0,
			counts: types.ReactionCounts{
				LikeCount:       10,
				InsightfulCount: 5,
				DisagreeCount:   2,
				FunnyCount:      3,
			},
		},
		{
			name:    "counts on a reply",
			postId:  1,
			replyId: 7,
			counts: types.ReactionCounts{
				LikeCount:       100,
				InsightfulCount: 50,
				DisagreeCount:   25,
				FunnyCount:      12,
			},
		},
		{
			name:    "all zero counts",
			postId:  2,
			replyId: 0,
			counts: types.ReactionCounts{
				LikeCount:       0,
				InsightfulCount: 0,
				DisagreeCount:   0,
				FunnyCount:      0,
			},
		},
		{
			name:    "single reaction type",
			postId:  3,
			replyId: 0,
			counts: types.ReactionCounts{
				LikeCount:       1,
				InsightfulCount: 0,
				DisagreeCount:   0,
				FunnyCount:      0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k.SetReactionCounts(ctx, tt.postId, tt.replyId, tt.counts)

			got := k.GetReactionCounts(ctx, tt.postId, tt.replyId)
			require.Equal(t, tt.counts.LikeCount, got.LikeCount)
			require.Equal(t, tt.counts.InsightfulCount, got.InsightfulCount)
			require.Equal(t, tt.counts.DisagreeCount, got.DisagreeCount)
			require.Equal(t, tt.counts.FunnyCount, got.FunnyCount)
		})
	}
}

func TestReactionCountsDefault(t *testing.T) {
	f := initFixture(t)
	ctx := f.ctx
	k := f.keeper

	tests := []struct {
		name    string
		postId  uint64
		replyId uint64
	}{
		{
			name:    "non-existent post",
			postId:  999,
			replyId: 0,
		},
		{
			name:    "non-existent reply",
			postId:  1,
			replyId: 888,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counts := k.GetReactionCounts(ctx, tt.postId, tt.replyId)
			require.Equal(t, uint64(0), counts.LikeCount)
			require.Equal(t, uint64(0), counts.InsightfulCount)
			require.Equal(t, uint64(0), counts.DisagreeCount)
			require.Equal(t, uint64(0), counts.FunnyCount)
		})
	}
}

func TestReactionCountsOverwrite(t *testing.T) {
	f := initFixture(t)
	ctx := f.ctx
	k := f.keeper

	initial := types.ReactionCounts{
		LikeCount:       5,
		InsightfulCount: 3,
		DisagreeCount:   1,
		FunnyCount:      0,
	}
	k.SetReactionCounts(ctx, 1, 0, initial)

	updated := types.ReactionCounts{
		LikeCount:       6,
		InsightfulCount: 3,
		DisagreeCount:   1,
		FunnyCount:      1,
	}
	k.SetReactionCounts(ctx, 1, 0, updated)

	got := k.GetReactionCounts(ctx, 1, 0)
	require.Equal(t, uint64(6), got.LikeCount)
	require.Equal(t, uint64(3), got.InsightfulCount)
	require.Equal(t, uint64(1), got.DisagreeCount)
	require.Equal(t, uint64(1), got.FunnyCount)
}

func TestReactionCountsIndependentTargets(t *testing.T) {
	f := initFixture(t)
	ctx := f.ctx
	k := f.keeper

	postCounts := types.ReactionCounts{
		LikeCount:       10,
		InsightfulCount: 0,
		DisagreeCount:   0,
		FunnyCount:      0,
	}
	replyCounts := types.ReactionCounts{
		LikeCount:       0,
		InsightfulCount: 20,
		DisagreeCount:   0,
		FunnyCount:      0,
	}

	k.SetReactionCounts(ctx, 1, 0, postCounts)
	k.SetReactionCounts(ctx, 1, 5, replyCounts)

	gotPost := k.GetReactionCounts(ctx, 1, 0)
	require.Equal(t, uint64(10), gotPost.LikeCount)
	require.Equal(t, uint64(0), gotPost.InsightfulCount)

	gotReply := k.GetReactionCounts(ctx, 1, 5)
	require.Equal(t, uint64(0), gotReply.LikeCount)
	require.Equal(t, uint64(20), gotReply.InsightfulCount)
}
