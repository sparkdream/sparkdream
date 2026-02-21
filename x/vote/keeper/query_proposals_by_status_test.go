package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vote/types"
)

func TestQueryProposalsByStatus(t *testing.T) {
	// helper to create a proposal with a given status
	makeProposal := func(id uint64, s types.ProposalStatus) types.VotingProposal {
		return types.VotingProposal{
			Id:            id,
			Title:         "Proposal",
			Proposer:      "proposer",
			Status:        s,
			Quorum:        math.LegacyNewDec(0),
			Threshold:     math.LegacyNewDec(0),
			VetoThreshold: math.LegacyNewDec(0),
		}
	}

	t.Run("matching: filter returns only matching status", func(t *testing.T) {
		f := initTestFixture(t)

		p := makeProposal(1, types.ProposalStatus_PROPOSAL_STATUS_ACTIVE)
		require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, 1, p))

		resp, err := f.queryServer.ProposalsByStatus(f.ctx, &types.QueryProposalsByStatusRequest{
			Status: uint64(types.ProposalStatus_PROPOSAL_STATUS_ACTIVE),
		})
		require.NoError(t, err)
		require.Len(t, resp.Proposals, 1)
		require.Equal(t, uint64(1), resp.Proposals[0].Id)
	})

	t.Run("non-matching: filter returns empty for non-matching status", func(t *testing.T) {
		f := initTestFixture(t)

		p := makeProposal(1, types.ProposalStatus_PROPOSAL_STATUS_ACTIVE)
		require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, 1, p))

		resp, err := f.queryServer.ProposalsByStatus(f.ctx, &types.QueryProposalsByStatusRequest{
			Status: uint64(types.ProposalStatus_PROPOSAL_STATUS_FINALIZED),
		})
		require.NoError(t, err)
		require.Empty(t, resp.Proposals)
	})

	t.Run("mixed: multiple statuses, filter selects correct ones", func(t *testing.T) {
		f := initTestFixture(t)

		proposals := []types.VotingProposal{
			makeProposal(1, types.ProposalStatus_PROPOSAL_STATUS_ACTIVE),
			makeProposal(2, types.ProposalStatus_PROPOSAL_STATUS_TALLYING),
			makeProposal(3, types.ProposalStatus_PROPOSAL_STATUS_ACTIVE),
			makeProposal(4, types.ProposalStatus_PROPOSAL_STATUS_FINALIZED),
			makeProposal(5, types.ProposalStatus_PROPOSAL_STATUS_CANCELLED),
		}
		for _, p := range proposals {
			require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, p.Id, p))
		}

		// Filter for ACTIVE: should return proposals 1 and 3
		resp, err := f.queryServer.ProposalsByStatus(f.ctx, &types.QueryProposalsByStatusRequest{
			Status: uint64(types.ProposalStatus_PROPOSAL_STATUS_ACTIVE),
		})
		require.NoError(t, err)
		require.Len(t, resp.Proposals, 2)

		ids := []uint64{resp.Proposals[0].Id, resp.Proposals[1].Id}
		require.Contains(t, ids, uint64(1))
		require.Contains(t, ids, uint64(3))

		// Filter for TALLYING: should return proposal 2
		resp, err = f.queryServer.ProposalsByStatus(f.ctx, &types.QueryProposalsByStatusRequest{
			Status: uint64(types.ProposalStatus_PROPOSAL_STATUS_TALLYING),
		})
		require.NoError(t, err)
		require.Len(t, resp.Proposals, 1)
		require.Equal(t, uint64(2), resp.Proposals[0].Id)

		// Filter for CANCELLED: should return proposal 5
		resp, err = f.queryServer.ProposalsByStatus(f.ctx, &types.QueryProposalsByStatusRequest{
			Status: uint64(types.ProposalStatus_PROPOSAL_STATUS_CANCELLED),
		})
		require.NoError(t, err)
		require.Len(t, resp.Proposals, 1)
		require.Equal(t, uint64(5), resp.Proposals[0].Id)
	})

	t.Run("nil request", func(t *testing.T) {
		f := initTestFixture(t)
		_, err := f.queryServer.ProposalsByStatus(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})
}
