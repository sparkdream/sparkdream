package keeper

import (
	"context"
	"errors"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
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

	// A proposal with no anonymous votes has no tally record: report zeros.
	anonTally, getErr := q.k.AnonVoteTallies.Get(ctx, proposal.Id)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		return nil, getErr
	}

	return &types.QueryGetProposalResponse{
		Proposal:  proposal,
		Votes:     votes,
		Tally:     tally,
		AnonTally: anonTally,
	}, nil
}

func (q queryServer) ListProposals(ctx context.Context, req *types.QueryListProposalsRequest) (*types.QueryListProposalsResponse, error) {
	if req == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "empty request")
	}

	if req.CouncilName != "" {
		// Filter by council using the index
		proposals, pageRes, err := query.CollectionPaginate(ctx, q.k.ProposalsByCouncil, req.Pagination,
			func(key collections.Pair[string, uint64], _ collections.NoValue) (types.Proposal, error) {
				return q.k.Proposals.Get(ctx, key.K2())
			},
			query.WithCollectionPaginationPairPrefix[string, uint64](req.CouncilName),
		)
		if err != nil {
			return nil, err
		}
		return &types.QueryListProposalsResponse{Proposals: proposals, Pagination: pageRes}, nil
	}

	proposals, pageRes, err := query.CollectionPaginate(ctx, q.k.Proposals, req.Pagination,
		func(_ uint64, proposal types.Proposal) (types.Proposal, error) {
			return proposal, nil
		},
	)
	if err != nil {
		return nil, err
	}
	return &types.QueryListProposalsResponse{Proposals: proposals, Pagination: pageRes}, nil
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

	anonTally, getErr := q.k.AnonVoteTallies.Get(ctx, proposal.Id)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		return nil, getErr
	}

	return &types.QueryGetProposalVotesResponse{
		Votes:     votes,
		Tally:     tally,
		AnonTally: anonTally,
	}, nil
}
