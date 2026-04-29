package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// stubBlogKeeper satisfies types.BlogKeeper for self-stake tests.
type stubBlogKeeper struct {
	authors map[uint64]string
}

func (s *stubBlogKeeper) GetPostAuthor(_ context.Context, postID uint64) (string, error) {
	a, ok := s.authors[postID]
	if !ok {
		return "", fmt.Errorf("blog post %d not found", postID)
	}
	return a, nil
}

// stubCollectKeeper satisfies types.CollectKeeper for self-stake tests.
type stubCollectKeeper struct {
	owners map[uint64]string
}

func (s *stubCollectKeeper) GetCollectionOwner(_ context.Context, collectionID uint64) (string, error) {
	o, ok := s.owners[collectionID]
	if !ok {
		return "", fmt.Errorf("collection %d not found", collectionID)
	}
	return o, nil
}

// TestSelfContentStake_BypassViaEmptyTargetIdentifier verifies that the author
// of content cannot stake on their own content even when targetIdentifier is
// empty (the previous bypass for REP-S2-7).
func TestSelfContentStake_BypassViaEmptyTargetIdentifier(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	author := sdk.AccAddress([]byte("author--------------"))
	other := sdk.AccAddress([]byte("other---------------"))

	// Author is the post creator.
	k.Member.Set(ctx, author.String(), types.Member{
		Address:          author.String(),
		DreamBalance:     PtrInt(math.NewInt(10000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: make(map[string]string),
	})
	k.Member.Set(ctx, other.String(), types.Member{
		Address:          other.String(),
		DreamBalance:     PtrInt(math.NewInt(10000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: make(map[string]string),
	})

	// Wire stub blog/forum/collect keepers with author lookups.
	const postID uint64 = 42
	const collID uint64 = 7
	k.SetBlogKeeper(&stubBlogKeeper{authors: map[uint64]string{postID: author.String()}})
	k.SetForumKeeper(&mockForumKeeper{authors: map[uint64]string{postID: author.String()}, tags: map[uint64][]string{}})
	k.SetCollectKeeper(&stubCollectKeeper{owners: map[uint64]string{collID: author.String()}})

	stakeAmt := math.NewInt(100)

	// Blog: empty targetIdentifier MUST still block author self-stake.
	_, err := k.CreateStake(ctx, author, types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT, postID, "", stakeAmt)
	require.ErrorIs(t, err, types.ErrSelfContentStake, "empty TargetIdentifier must not bypass blog self-stake check")

	// Blog: forged (non-empty, wrong) targetIdentifier MUST still block.
	_, err = k.CreateStake(ctx, author, types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT, postID, other.String(), stakeAmt)
	require.ErrorIs(t, err, types.ErrSelfContentStake, "forged TargetIdentifier must not bypass blog self-stake check")

	// Forum: empty targetIdentifier MUST still block.
	_, err = k.CreateStake(ctx, author, types.StakeTargetType_STAKE_TARGET_FORUM_CONTENT, postID, "", stakeAmt)
	require.ErrorIs(t, err, types.ErrSelfContentStake, "empty TargetIdentifier must not bypass forum self-stake check")

	// Collect: empty targetIdentifier MUST still block.
	_, err = k.CreateStake(ctx, author, types.StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT, collID, "", stakeAmt)
	require.ErrorIs(t, err, types.ErrSelfContentStake, "empty TargetIdentifier must not bypass collection self-stake check")

	// A different member CAN stake on the author's content.
	_, err = k.CreateStake(ctx, other, types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT, postID, "", stakeAmt)
	require.NoError(t, err, "non-author should be able to stake on content")
}
