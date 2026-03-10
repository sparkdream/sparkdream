package keeper

import (
	"context"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (q queryServer) GetProposal(ctx context.Context, req *types.QueryGetProposalRequest) (*types.QueryGetProposalResponse, error) {
	if req == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "empty request")
	}

	proposal, err := q.k.Proposals.Get(ctx, req.ProposalId)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound, "proposal %d not found", req.ProposalId)
	}

	// Get votes
	var votes []types.Vote
	rng := collections.NewPrefixedPairRange[uint64, string](req.ProposalId)
	err = q.k.Votes.Walk(ctx, rng, func(_ collections.Pair[uint64, string], vote types.Vote) (bool, error) {
		votes = append(votes, vote)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Compute tally
	tally, err := q.k.TallyProposal(ctx, proposal)
	if err != nil {
		tally = types.TallyResult{}
	}

	return &types.QueryGetProposalResponse{
		Proposal: proposal,
		Votes:    votes,
		Tally:    tally,
	}, nil
}

func (q queryServer) ListProposals(ctx context.Context, req *types.QueryListProposalsRequest) (*types.QueryListProposalsResponse, error) {
	if req == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "empty request")
	}

	var proposals []types.Proposal

	if req.CouncilName != "" {
		// Filter by council using the index
		rng := collections.NewPrefixedPairRange[string, uint64](req.CouncilName)
		err := q.k.ProposalsByCouncil.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
			proposal, err := q.k.Proposals.Get(ctx, key.K2())
			if err != nil {
				return false, nil // Skip missing proposals
			}
			proposals = append(proposals, proposal)
			return false, nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		// Return all proposals
		err := q.k.Proposals.Walk(ctx, nil, func(_ uint64, proposal types.Proposal) (bool, error) {
			proposals = append(proposals, proposal)
			return false, nil
		})
		if err != nil {
			return nil, err
		}
	}

	return &types.QueryListProposalsResponse{Proposals: proposals}, nil
}

func (q queryServer) GetProposalVotes(ctx context.Context, req *types.QueryGetProposalVotesRequest) (*types.QueryGetProposalVotesResponse, error) {
	if req == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "empty request")
	}

	// Verify proposal exists
	proposal, err := q.k.Proposals.Get(ctx, req.ProposalId)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound, "proposal %d not found", req.ProposalId)
	}

	var votes []types.Vote
	rng := collections.NewPrefixedPairRange[uint64, string](req.ProposalId)
	err = q.k.Votes.Walk(ctx, rng, func(_ collections.Pair[uint64, string], vote types.Vote) (bool, error) {
		votes = append(votes, vote)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	tally, err := q.k.TallyProposal(ctx, proposal)
	if err != nil {
		tally = types.TallyResult{}
	}

	return &types.QueryGetProposalVotesResponse{
		Votes: votes,
		Tally: tally,
	}, nil
}
