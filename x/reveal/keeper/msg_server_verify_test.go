package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/reveal/keeper"
	"sparkdream/x/reveal/types"
)

func TestMsgVerify_Success(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 1000)
	f.revealTranche(t, contribID, 0)

	_, err := f.msgServer.Verify(f.ctx, &types.MsgVerify{
		Voter:          f.staker,
		ContributionId: contribID,
		TrancheId:      0,
		ValueConfirmed: true,
		QualityRating:  4,
		Comments:       "Well done",
	})
	require.NoError(t, err)

	// Verify vote stored
	vk := keeper.VoteKey(contribID, 0, f.staker)
	vote, err := f.keeper.Vote.Get(f.ctx, vk)
	require.NoError(t, err)
	require.True(t, vote.ValueConfirmed)
	require.Equal(t, uint32(4), vote.QualityRating)
	require.Equal(t, math.NewInt(1000), vote.StakeWeight)
}

func TestMsgVerify_SelfVote(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 1000)
	f.revealTranche(t, contribID, 0)

	_, err := f.msgServer.Verify(f.ctx, &types.MsgVerify{
		Voter:          f.contributor, // contributor voting on own
		ContributionId: contribID,
		TrancheId:      0,
		ValueConfirmed: true,
		QualityRating:  5,
	})
	require.ErrorIs(t, err, types.ErrSelfVote)
}

func TestMsgVerify_AlreadyVoted(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 1000)
	f.revealTranche(t, contribID, 0)

	f.castVerifyVote(t, contribID, 0, f.staker, true, 4)

	_, err := f.msgServer.Verify(f.ctx, &types.MsgVerify{
		Voter:          f.staker,
		ContributionId: contribID,
		TrancheId:      0,
		ValueConfirmed: false,
		QualityRating:  1,
	})
	require.ErrorIs(t, err, types.ErrAlreadyVoted)
}

func TestMsgVerify_InvalidQualityRating(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.msgServer.Verify(f.ctx, &types.MsgVerify{
		Voter:          f.staker,
		ContributionId: 1,
		TrancheId:      0,
		ValueConfirmed: true,
		QualityRating:  0, // invalid: must be 1-5
	})
	require.ErrorIs(t, err, types.ErrInvalidQualityRating)

	_, err = f.msgServer.Verify(f.ctx, &types.MsgVerify{
		Voter:          f.staker,
		ContributionId: 1,
		TrancheId:      0,
		ValueConfirmed: true,
		QualityRating:  6, // invalid: must be 1-5
	})
	require.ErrorIs(t, err, types.ErrInvalidQualityRating)
}

func TestMsgVerify_NotStaker(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 1000)
	f.revealTranche(t, contribID, 0)

	// staker2 has no stake on this tranche
	_, err := f.msgServer.Verify(f.ctx, &types.MsgVerify{
		Voter:          f.staker2,
		ContributionId: contribID,
		TrancheId:      0,
		ValueConfirmed: true,
		QualityRating:  3,
	})
	require.ErrorIs(t, err, types.ErrNotStaker)
}

func TestMsgVerify_TrancheNotRevealed(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 1000)
	// Don't reveal

	_, err := f.msgServer.Verify(f.ctx, &types.MsgVerify{
		Voter:          f.staker,
		ContributionId: contribID,
		TrancheId:      0,
		ValueConfirmed: true,
		QualityRating:  3,
	})
	require.ErrorIs(t, err, types.ErrTrancheNotRevealed)
}
