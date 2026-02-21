package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vote/types"
)

func TestQueryProposalsByType(t *testing.T) {
	// helper to create a proposal with a given type
	makeProposal := func(id uint64, pt types.ProposalType) types.VotingProposal {
		return types.VotingProposal{
			Id:            id,
			Title:         "Proposal",
			Proposer:      "proposer",
			Status:        types.ProposalStatus_PROPOSAL_STATUS_ACTIVE,
			ProposalType:  pt,
			Quorum:        math.LegacyNewDec(0),
			Threshold:     math.LegacyNewDec(0),
			VetoThreshold: math.LegacyNewDec(0),
		}
	}

	t.Run("matching: filter returns only matching type", func(t *testing.T) {
		f := initTestFixture(t)

		p := makeProposal(1, types.ProposalType_PROPOSAL_TYPE_GENERAL)
		require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, 1, p))

		resp, err := f.queryServer.ProposalsByType(f.ctx, &types.QueryProposalsByTypeRequest{
			ProposalType: uint64(types.ProposalType_PROPOSAL_TYPE_GENERAL),
		})
		require.NoError(t, err)
		require.Len(t, resp.Proposals, 1)
		require.Equal(t, uint64(1), resp.Proposals[0].Id)
	})

	t.Run("non-matching: filter returns empty for non-matching type", func(t *testing.T) {
		f := initTestFixture(t)

		p := makeProposal(1, types.ProposalType_PROPOSAL_TYPE_GENERAL)
		require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, 1, p))

		resp, err := f.queryServer.ProposalsByType(f.ctx, &types.QueryProposalsByTypeRequest{
			ProposalType: uint64(types.ProposalType_PROPOSAL_TYPE_SLASHING),
		})
		require.NoError(t, err)
		require.Empty(t, resp.Proposals)
	})

	t.Run("mixed: multiple types, filter selects correct ones", func(t *testing.T) {
		f := initTestFixture(t)

		proposals := []types.VotingProposal{
			makeProposal(1, types.ProposalType_PROPOSAL_TYPE_GENERAL),
			makeProposal(2, types.ProposalType_PROPOSAL_TYPE_PARAMETER_CHANGE),
			makeProposal(3, types.ProposalType_PROPOSAL_TYPE_GENERAL),
			makeProposal(4, types.ProposalType_PROPOSAL_TYPE_COUNCIL_ELECTION),
			makeProposal(5, types.ProposalType_PROPOSAL_TYPE_SLASHING),
		}
		for _, p := range proposals {
			require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, p.Id, p))
		}

		// Filter for GENERAL: should return proposals 1 and 3
		resp, err := f.queryServer.ProposalsByType(f.ctx, &types.QueryProposalsByTypeRequest{
			ProposalType: uint64(types.ProposalType_PROPOSAL_TYPE_GENERAL),
		})
		require.NoError(t, err)
		require.Len(t, resp.Proposals, 2)

		ids := []uint64{resp.Proposals[0].Id, resp.Proposals[1].Id}
		require.Contains(t, ids, uint64(1))
		require.Contains(t, ids, uint64(3))

		// Filter for PARAMETER_CHANGE: should return proposal 2
		resp, err = f.queryServer.ProposalsByType(f.ctx, &types.QueryProposalsByTypeRequest{
			ProposalType: uint64(types.ProposalType_PROPOSAL_TYPE_PARAMETER_CHANGE),
		})
		require.NoError(t, err)
		require.Len(t, resp.Proposals, 1)
		require.Equal(t, uint64(2), resp.Proposals[0].Id)

		// Filter for COUNCIL_ELECTION: should return proposal 4
		resp, err = f.queryServer.ProposalsByType(f.ctx, &types.QueryProposalsByTypeRequest{
			ProposalType: uint64(types.ProposalType_PROPOSAL_TYPE_COUNCIL_ELECTION),
		})
		require.NoError(t, err)
		require.Len(t, resp.Proposals, 1)
		require.Equal(t, uint64(4), resp.Proposals[0].Id)
	})

	t.Run("nil request", func(t *testing.T) {
		f := initTestFixture(t)
		_, err := f.queryServer.ProposalsByType(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})
}
