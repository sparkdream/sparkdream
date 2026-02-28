package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgRespondToContentChallenge_EmptyResponseForfeits(t *testing.T) {
	f, authorAddr, challengerAddr := setupContentChallengeFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Create a challenge first
	stakeAmount := math.NewInt(100000000)
	ccID, err := f.keeper.CreateContentChallenge(
		f.ctx,
		challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		"Bad content",
		[]string{"evidence"},
		stakeAmount,
	)
	require.NoError(t, err)

	// Author responds with empty response => forfeit (auto-uphold)
	msg := &types.MsgRespondToContentChallenge{
		Author:             authorAddr.String(),
		ContentChallengeId: ccID,
		Response:           "",
		Evidence:           nil,
	}

	resp, err := msgServer.RespondToContentChallenge(f.ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify challenge was upheld
	cc, err := f.keeper.ContentChallenge.Get(f.ctx, ccID)
	require.NoError(t, err)
	require.Equal(t, types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_UPHELD, cc.Status)
}

func TestMsgRespondToContentChallenge_InvalidAddress(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	msg := &types.MsgRespondToContentChallenge{
		Author:             "invalid_address",
		ContentChallengeId: 1,
		Response:           "My response",
		Evidence:           nil,
	}

	_, err := msgServer.RespondToContentChallenge(f.ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid author address")
}

func TestMsgRespondToContentChallenge_ChallengeNotFound(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	authorAddr := sdk.AccAddress([]byte("author_respond______"))
	msg := &types.MsgRespondToContentChallenge{
		Author:             authorAddr.String(),
		ContentChallengeId: 999, // Nonexistent
		Response:           "My response",
		Evidence:           nil,
	}

	_, err := msgServer.RespondToContentChallenge(f.ctx, msg)
	require.ErrorIs(t, err, types.ErrContentChallengeNotFound)
}

func TestMsgRespondToContentChallenge_WrongAuthor(t *testing.T) {
	f, _, challengerAddr := setupContentChallengeFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Create a challenge
	stakeAmount := math.NewInt(100000000)
	ccID, err := f.keeper.CreateContentChallenge(
		f.ctx,
		challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		"Bad content",
		nil,
		stakeAmount,
	)
	require.NoError(t, err)

	// Wrong person responds
	wrongAddr := sdk.AccAddress([]byte("wrong_author________"))
	msg := &types.MsgRespondToContentChallenge{
		Author:             wrongAddr.String(),
		ContentChallengeId: ccID,
		Response:           "I claim to be the author",
		Evidence:           nil,
	}

	_, err = msgServer.RespondToContentChallenge(f.ctx, msg)
	require.ErrorIs(t, err, types.ErrNotContentAuthor)
}

func TestMsgRespondToContentChallenge_NotActive(t *testing.T) {
	f, authorAddr, challengerAddr := setupContentChallengeFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Create and uphold a challenge
	stakeAmount := math.NewInt(100000000)
	ccID, err := f.keeper.CreateContentChallenge(
		f.ctx,
		challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		"Bad content",
		nil,
		stakeAmount,
	)
	require.NoError(t, err)

	err = f.keeper.UpholdContentChallenge(f.ctx, ccID)
	require.NoError(t, err)

	// Try to respond to already-resolved challenge
	msg := &types.MsgRespondToContentChallenge{
		Author:             authorAddr.String(),
		ContentChallengeId: ccID,
		Response:           "Too late",
		Evidence:           nil,
	}

	_, err = msgServer.RespondToContentChallenge(f.ctx, msg)
	require.ErrorIs(t, err, types.ErrContentChallengeNotActive)
}

func TestMsgRespondToContentChallenge_WithEvidence(t *testing.T) {
	f, authorAddr, challengerAddr := setupContentChallengeFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Add jurors for jury review creation
	juror := sdk.AccAddress([]byte("juror_respond_______"))
	jurorMember := types.Member{
		Address:          juror.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		ReputationScores: map[string]string{"general": "100.0"},
	}
	require.NoError(t, f.keeper.Member.Set(f.ctx, jurorMember.Address, jurorMember))

	// Set small jury size for test
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.JurySize = 1
	params.MinJurorReputation = math.LegacyOneDec()
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Create a challenge
	stakeAmount := math.NewInt(100000000)
	ccID, err := f.keeper.CreateContentChallenge(
		f.ctx,
		challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		"Bad content",
		nil,
		stakeAmount,
	)
	require.NoError(t, err)

	// Author responds with evidence => routes to jury
	msg := &types.MsgRespondToContentChallenge{
		Author:             authorAddr.String(),
		ContentChallengeId: ccID,
		Response:           "I disagree, here is my evidence",
		Evidence:           []string{"defense_evidence_1", "defense_evidence_2"},
	}

	resp, err := msgServer.RespondToContentChallenge(f.ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify challenge is now in jury review
	cc, err := f.keeper.ContentChallenge.Get(f.ctx, ccID)
	require.NoError(t, err)
	require.Equal(t, types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_IN_JURY_REVIEW, cc.Status)
	require.Equal(t, "I disagree, here is my evidence", cc.AuthorResponse)
	require.Equal(t, []string{"defense_evidence_1", "defense_evidence_2"}, cc.AuthorEvidence)
	require.NotZero(t, cc.JuryReviewId)
}
