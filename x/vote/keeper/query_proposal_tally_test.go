package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vote/types"
)

func TestQueryProposalTally(t *testing.T) {
	t.Run("correct tally: proposal with votes", func(t *testing.T) {
		f := initTestFixture(t)

		proposal := types.VotingProposal{
			Id:       1,
			Title:    "Tally Test",
			Proposer: f.member,
			Status:   types.ProposalStatus_PROPOSAL_STATUS_ACTIVE,
			Tally: []*types.VoteTally{
				{OptionId: 0, VoteCount: 5},
				{OptionId: 1, VoteCount: 3},
			},
			EligibleVoters: 20,
			Quorum:         math.LegacyNewDec(0),
			Threshold:      math.LegacyNewDec(0),
			VetoThreshold:  math.LegacyNewDec(0),
		}
		require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, 1, proposal))

		resp, err := f.queryServer.ProposalTally(f.ctx, &types.QueryProposalTallyRequest{
			ProposalId: 1,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.Tally, 2)
		require.Equal(t, uint64(8), resp.TotalVotes)
		require.Equal(t, uint64(20), resp.EligibleVoters)
		require.Equal(t, uint64(5), resp.Tally[0].VoteCount)
		require.Equal(t, uint64(3), resp.Tally[1].VoteCount)
	})

	t.Run("zero tally: proposal with no votes", func(t *testing.T) {
		f := initTestFixture(t)

		proposal := types.VotingProposal{
			Id:       2,
			Title:    "Zero Tally",
			Proposer: f.member,
			Status:   types.ProposalStatus_PROPOSAL_STATUS_ACTIVE,
			Tally: []*types.VoteTally{
				{OptionId: 0, VoteCount: 0},
				{OptionId: 1, VoteCount: 0},
			},
			EligibleVoters: 15,
			Quorum:         math.LegacyNewDec(0),
			Threshold:      math.LegacyNewDec(0),
			VetoThreshold:  math.LegacyNewDec(0),
		}
		require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, 2, proposal))

		resp, err := f.queryServer.ProposalTally(f.ctx, &types.QueryProposalTallyRequest{
			ProposalId: 2,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, uint64(0), resp.TotalVotes)
		require.Equal(t, uint64(15), resp.EligibleVoters)
	})

	t.Run("not found: non-existent proposal", func(t *testing.T) {
		f := initTestFixture(t)

		_, err := f.queryServer.ProposalTally(f.ctx, &types.QueryProposalTallyRequest{
			ProposalId: 999,
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("nil request", func(t *testing.T) {
		f := initTestFixture(t)

		_, err := f.queryServer.ProposalTally(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})
}
