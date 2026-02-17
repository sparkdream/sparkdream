package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/reveal/types"
)

func TestQueryTrancheTally(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 600)
	f.stakeOnTranche(t, contribID, 0, f.staker2, 400)
	f.revealTranche(t, contribID, 0)

	f.castVerifyVote(t, contribID, 0, f.staker, true, 4)
	f.castVerifyVote(t, contribID, 0, f.staker2, false, 2)

	resp, err := f.queryServer.TrancheTally(f.ctx, &types.QueryTrancheTallyRequest{
		ContributionId: contribID,
		TrancheId:      0,
	})
	require.NoError(t, err)
	require.Equal(t, math.NewInt(600), resp.YesWeight)
	require.Equal(t, math.NewInt(400), resp.NoWeight)
	require.Equal(t, uint32(2), resp.VoteCount)
}

func TestQueryTrancheTally_NoVotes(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)

	resp, err := f.queryServer.TrancheTally(f.ctx, &types.QueryTrancheTallyRequest{
		ContributionId: contribID,
		TrancheId:      0,
	})
	require.NoError(t, err)
	require.Equal(t, math.ZeroInt(), resp.YesWeight)
	require.Equal(t, math.ZeroInt(), resp.NoWeight)
	require.Equal(t, uint32(0), resp.VoteCount)
}

func TestQueryTrancheTally_ContributionNotFound(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.TrancheTally(f.ctx, &types.QueryTrancheTallyRequest{
		ContributionId: 9999,
		TrancheId:      0,
	})
	require.Error(t, err)
}

func TestQueryTrancheTally_NilRequest(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.TrancheTally(f.ctx, nil)
	require.Error(t, err)
}
