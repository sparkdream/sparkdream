package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/reveal/types"
)

func TestQueryVotesByVoter(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 1000)
	f.revealTranche(t, contribID, 0)

	f.castVerifyVote(t, contribID, 0, f.staker, true, 5)

	resp, err := f.queryServer.VotesByVoter(f.ctx, &types.QueryVotesByVoterRequest{
		Voter: f.staker,
	})
	require.NoError(t, err)
	require.Equal(t, 1, len(resp.Votes))
	require.True(t, resp.Votes[0].ValueConfirmed)
}

func TestQueryVotesByVoter_NoVotes(t *testing.T) {
	f := initTestFixture(t)

	resp, err := f.queryServer.VotesByVoter(f.ctx, &types.QueryVotesByVoterRequest{
		Voter: f.staker,
	})
	require.NoError(t, err)
	require.Empty(t, resp.Votes)
}

func TestQueryVotesByVoter_NilRequest(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.VotesByVoter(f.ctx, nil)
	require.Error(t, err)
}
