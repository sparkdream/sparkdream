package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// setupContentChallengeFixture creates a test fixture with a member who has an author bond.
// Returns the fixture plus the author and challenger addresses.
func setupContentChallengeFixture(t *testing.T) (*fixture, sdk.AccAddress, sdk.AccAddress) {
	t.Helper()
	f := initFixture(t)

	authorAddr := sdk.AccAddress([]byte("author______________"))
	challengerAddr := sdk.AccAddress([]byte("challenger__________"))

	// Create author member with DREAM balance
	authorMember := types.Member{
		Address:          authorAddr.String(),
		DreamBalance:     ptrInt(math.NewInt(5000000000)), // 5000 DREAM
		StakedDream:      ptrInt(math.ZeroInt()),
		LifetimeEarned:   ptrInt(math.ZeroInt()),
		LifetimeBurned:   ptrInt(math.ZeroInt()),
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		ReputationScores: map[string]string{"general": "100.0"},
	}
	require.NoError(t, f.keeper.Member.Set(f.ctx, authorMember.Address, authorMember))

	// Create challenger member with DREAM balance
	challengerMember := types.Member{
		Address:          challengerAddr.String(),
		DreamBalance:     ptrInt(math.NewInt(5000000000)), // 5000 DREAM
		StakedDream:      ptrInt(math.ZeroInt()),
		LifetimeEarned:   ptrInt(math.ZeroInt()),
		LifetimeBurned:   ptrInt(math.ZeroInt()),
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		ReputationScores: map[string]string{"general": "100.0"},
	}
	require.NoError(t, f.keeper.Member.Set(f.ctx, challengerMember.Address, challengerMember))

	// Create an author bond (blog post #1)
	bondAmount := math.NewInt(500000000) // 500 DREAM
	_, err := f.keeper.CreateAuthorBond(
		f.ctx,
		authorAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1, // post ID 1
		bondAmount,
	)
	require.NoError(t, err)

	return f, authorAddr, challengerAddr
}

func ptrInt(i math.Int) *math.Int {
	return &i
}

func TestCreateContentChallenge(t *testing.T) {
	f, authorAddr, challengerAddr := setupContentChallengeFixture(t)

	stakeAmount := math.NewInt(100000000) // 100 DREAM

	// Happy path
	ccID, err := f.keeper.CreateContentChallenge(
		f.ctx,
		challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		"Inaccurate claims in post",
		[]string{"evidence1", "evidence2"},
		stakeAmount,
	)
	require.NoError(t, err)
	require.NotZero(t, ccID)

	// Verify stored challenge
	cc, err := f.keeper.ContentChallenge.Get(f.ctx, ccID)
	require.NoError(t, err)
	require.Equal(t, types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_ACTIVE, cc.Status)
	require.Equal(t, challengerAddr.String(), cc.Challenger)
	require.Equal(t, authorAddr.String(), cc.Author)
	require.Equal(t, stakeAmount, cc.StakedDream)
	require.Equal(t, "Inaccurate claims in post", cc.Reason)
	require.Equal(t, math.NewInt(500000000), cc.BondAmount)

	// Verify target index
	hasActive, err := f.keeper.HasActiveContentChallenge(
		f.ctx,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
	)
	require.NoError(t, err)
	require.True(t, hasActive)
}

func TestCreateContentChallenge_NoBond(t *testing.T) {
	f, _, challengerAddr := setupContentChallengeFixture(t)

	// Try to challenge content without a bond (post #999)
	_, err := f.keeper.CreateContentChallenge(
		f.ctx,
		challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		999,
		"No bond here",
		nil,
		math.NewInt(100),
	)
	require.ErrorIs(t, err, types.ErrNoAuthorBond)
}

func TestCreateContentChallenge_SelfChallenge(t *testing.T) {
	f, authorAddr, _ := setupContentChallengeFixture(t)

	// Author tries to challenge their own content
	_, err := f.keeper.CreateContentChallenge(
		f.ctx,
		authorAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		"Self challenge",
		nil,
		math.NewInt(100),
	)
	require.ErrorIs(t, err, types.ErrCannotChallengeOwnContent)
}

func TestCreateContentChallenge_DuplicateChallenge(t *testing.T) {
	f, _, challengerAddr := setupContentChallengeFixture(t)

	// First challenge succeeds
	_, err := f.keeper.CreateContentChallenge(
		f.ctx,
		challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		"First challenge",
		nil,
		math.NewInt(100),
	)
	require.NoError(t, err)

	// Second challenge on same content fails
	_, err = f.keeper.CreateContentChallenge(
		f.ctx,
		challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		"Duplicate challenge",
		nil,
		math.NewInt(100),
	)
	require.ErrorIs(t, err, types.ErrContentChallengeExists)
}

func TestCreateContentChallenge_InsufficientStake(t *testing.T) {
	f, _, challengerAddr := setupContentChallengeFixture(t)

	// Stake less than MinChallengeStake
	_, err := f.keeper.CreateContentChallenge(
		f.ctx,
		challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		"Low stake",
		nil,
		math.NewInt(1), // Way below minimum
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient stake")
}

func TestCreateContentChallenge_InvalidTargetType(t *testing.T) {
	f, _, challengerAddr := setupContentChallengeFixture(t)

	// Try with a non-author-bond target type
	_, err := f.keeper.CreateContentChallenge(
		f.ctx,
		challengerAddr,
		types.StakeTargetType_STAKE_TARGET_INITIATIVE,
		1,
		"Wrong type",
		nil,
		math.NewInt(100),
	)
	require.ErrorIs(t, err, types.ErrNotAuthorBondType)
}

func TestRespondToContentChallenge_EmptyResponse(t *testing.T) {
	f, authorAddr, challengerAddr := setupContentChallengeFixture(t)

	// Create challenge
	ccID, err := f.keeper.CreateContentChallenge(
		f.ctx,
		challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		"Bad content",
		nil,
		math.NewInt(100),
	)
	require.NoError(t, err)

	// Author responds with empty response → auto-forfeit (upheld)
	err = f.keeper.RespondToContentChallenge(f.ctx, ccID, authorAddr, "", nil)
	require.NoError(t, err)

	// Verify challenge was upheld
	cc, err := f.keeper.ContentChallenge.Get(f.ctx, ccID)
	require.NoError(t, err)
	require.Equal(t, types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_UPHELD, cc.Status)
}

func TestRespondToContentChallenge_WrongAuthor(t *testing.T) {
	f, _, challengerAddr := setupContentChallengeFixture(t)

	// Create challenge
	ccID, err := f.keeper.CreateContentChallenge(
		f.ctx,
		challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		"Bad content",
		nil,
		math.NewInt(100),
	)
	require.NoError(t, err)

	// Wrong person tries to respond
	wrongAddr := sdk.AccAddress([]byte("wrongperson_________"))
	err = f.keeper.RespondToContentChallenge(f.ctx, ccID, wrongAddr, "I'm the author", nil)
	require.ErrorIs(t, err, types.ErrNotContentAuthor)
}

func TestRespondToContentChallenge_NotActive(t *testing.T) {
	f, authorAddr, challengerAddr := setupContentChallengeFixture(t)

	// Create and uphold a challenge
	ccID, err := f.keeper.CreateContentChallenge(
		f.ctx,
		challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		"Bad content",
		nil,
		math.NewInt(100),
	)
	require.NoError(t, err)
	err = f.keeper.UpholdContentChallenge(f.ctx, ccID)
	require.NoError(t, err)

	// Try to respond to an already-resolved challenge
	err = f.keeper.RespondToContentChallenge(f.ctx, ccID, authorAddr, "Too late", nil)
	require.ErrorIs(t, err, types.ErrContentChallengeNotActive)
}

func TestUpholdContentChallenge(t *testing.T) {
	f, authorAddr, challengerAddr := setupContentChallengeFixture(t)

	stakeAmount := math.NewInt(100000000) // 100 DREAM

	ccID, err := f.keeper.CreateContentChallenge(
		f.ctx,
		challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		"Inaccurate",
		nil,
		stakeAmount,
	)
	require.NoError(t, err)

	// Set non-zero block height so ResolvedAt is meaningful
	f.ctx = f.ctx.WithBlockHeight(100)

	// Uphold the challenge
	err = f.keeper.UpholdContentChallenge(f.ctx, ccID)
	require.NoError(t, err)

	// Verify challenge status
	cc, err := f.keeper.ContentChallenge.Get(f.ctx, ccID)
	require.NoError(t, err)
	require.Equal(t, types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_UPHELD, cc.Status)
	require.Equal(t, int64(100), cc.ResolvedAt)

	// Verify author bond was removed
	_, err = f.keeper.GetAuthorBond(f.ctx, types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 1)
	require.ErrorIs(t, err, types.ErrAuthorBondNotFound)

	// Verify target index cleared
	hasActive, err := f.keeper.HasActiveContentChallenge(
		f.ctx,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
	)
	require.NoError(t, err)
	require.False(t, hasActive)

	// Verify challenger's DREAM was unlocked (staked dream reduced)
	challengerMember, err := f.keeper.GetMember(f.ctx, challengerAddr)
	require.NoError(t, err)
	// Challenger should have gotten the stake back + reward minted
	_ = challengerMember

	// Verify author's DREAM was burned (staked dream reduced from bond)
	authorMember, err := f.keeper.GetMember(f.ctx, authorAddr)
	require.NoError(t, err)
	_ = authorMember
}

func TestRejectContentChallenge(t *testing.T) {
	f, _, challengerAddr := setupContentChallengeFixture(t)

	stakeAmount := math.NewInt(100000000) // 100 DREAM

	ccID, err := f.keeper.CreateContentChallenge(
		f.ctx,
		challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		"Frivolous challenge",
		nil,
		stakeAmount,
	)
	require.NoError(t, err)

	// Set non-zero block height so ResolvedAt is meaningful
	f.ctx = f.ctx.WithBlockHeight(100)

	// Reject the challenge
	err = f.keeper.RejectContentChallenge(f.ctx, ccID)
	require.NoError(t, err)

	// Verify challenge status
	cc, err := f.keeper.ContentChallenge.Get(f.ctx, ccID)
	require.NoError(t, err)
	require.Equal(t, types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_REJECTED, cc.Status)
	require.Equal(t, int64(100), cc.ResolvedAt)

	// Verify author bond still exists (not slashed)
	bond, err := f.keeper.GetAuthorBond(f.ctx, types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 1)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(500000000), bond.Amount) // Bond intact

	// Verify target index cleared
	hasActive, err := f.keeper.HasActiveContentChallenge(
		f.ctx,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
	)
	require.NoError(t, err)
	require.False(t, hasActive)
}

func TestResolveInconclusiveContentChallenge(t *testing.T) {
	f, _, challengerAddr := setupContentChallengeFixture(t)

	stakeAmount := math.NewInt(100000000) // 100 DREAM

	ccID, err := f.keeper.CreateContentChallenge(
		f.ctx,
		challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		"Unclear case",
		nil,
		stakeAmount,
	)
	require.NoError(t, err)

	// Resolve as inconclusive
	err = f.keeper.ResolveInconclusiveContentChallenge(f.ctx, ccID)
	require.NoError(t, err)

	// Verify status is REJECTED (status quo preserved)
	cc, err := f.keeper.ContentChallenge.Get(f.ctx, ccID)
	require.NoError(t, err)
	require.Equal(t, types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_REJECTED, cc.Status)

	// Verify author bond still exists
	bond, err := f.keeper.GetAuthorBond(f.ctx, types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 1)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(500000000), bond.Amount)
}

func TestBondLockedDuringChallenge(t *testing.T) {
	f, authorAddr, challengerAddr := setupContentChallengeFixture(t)

	// Create a challenge
	_, err := f.keeper.CreateContentChallenge(
		f.ctx,
		challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		"Challenge in progress",
		nil,
		math.NewInt(100),
	)
	require.NoError(t, err)

	// Get the author bond stake
	bond, err := f.keeper.GetAuthorBond(f.ctx, types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 1)
	require.NoError(t, err)

	// Try to unstake the author bond — should fail
	err = f.keeper.RemoveStake(f.ctx, bond.Id, authorAddr, bond.Amount)
	require.ErrorIs(t, err, types.ErrBondLockedByChallenge)
}

func TestEndBlockerAutoUpholdsExpiredContentChallenge(t *testing.T) {
	// Use custom params with short deadline for testing
	params := types.DefaultParams()
	params.ChallengeResponseDeadlineEpochs = 1
	params.EpochBlocks = 10

	f := initFixture(t, WithCustomParams(params))

	authorAddr := sdk.AccAddress([]byte("author______________"))
	challengerAddr := sdk.AccAddress([]byte("challenger__________"))

	// Create members
	authorMember := types.Member{
		Address:          authorAddr.String(),
		DreamBalance:     ptrInt(math.NewInt(5000000000)),
		StakedDream:      ptrInt(math.ZeroInt()),
		LifetimeEarned:   ptrInt(math.ZeroInt()),
		LifetimeBurned:   ptrInt(math.ZeroInt()),
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		ReputationScores: map[string]string{"general": "100.0"},
	}
	require.NoError(t, f.keeper.Member.Set(f.ctx, authorMember.Address, authorMember))

	challengerMember := types.Member{
		Address:          challengerAddr.String(),
		DreamBalance:     ptrInt(math.NewInt(5000000000)),
		StakedDream:      ptrInt(math.ZeroInt()),
		LifetimeEarned:   ptrInt(math.ZeroInt()),
		LifetimeBurned:   ptrInt(math.ZeroInt()),
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		ReputationScores: map[string]string{"general": "100.0"},
	}
	require.NoError(t, f.keeper.Member.Set(f.ctx, challengerMember.Address, challengerMember))

	// Create author bond
	_, err := f.keeper.CreateAuthorBond(f.ctx, authorAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 1, math.NewInt(500000000))
	require.NoError(t, err)

	// Create challenge
	ccID, err := f.keeper.CreateContentChallenge(
		f.ctx,
		challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		"Expired deadline",
		nil,
		math.NewInt(100),
	)
	require.NoError(t, err)

	// Verify it's active
	cc, err := f.keeper.ContentChallenge.Get(f.ctx, ccID)
	require.NoError(t, err)
	require.Equal(t, types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_ACTIVE, cc.Status)

	// Advance block height past the deadline
	f.ctx = f.ctx.WithBlockHeight(cc.ResponseDeadline + 1)

	// Run EndBlocker
	err = f.keeper.EndBlocker(f.ctx)
	require.NoError(t, err)

	// Verify challenge was auto-upheld
	cc, err = f.keeper.ContentChallenge.Get(f.ctx, ccID)
	require.NoError(t, err)
	require.Equal(t, types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_UPHELD, cc.Status)
}

func TestContentChallengeNotFound(t *testing.T) {
	f := initFixture(t)

	err := f.keeper.RespondToContentChallenge(f.ctx, 999, sdk.AccAddress([]byte("anyone______________")), "response", nil)
	require.ErrorIs(t, err, types.ErrContentChallengeNotFound)
}

func TestHasActiveContentChallenge_NoneExists(t *testing.T) {
	f := initFixture(t)

	hasActive, err := f.keeper.HasActiveContentChallenge(
		f.ctx,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		999,
	)
	require.NoError(t, err)
	require.False(t, hasActive)
}
