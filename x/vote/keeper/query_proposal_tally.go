package keeper

import (
	"context"

	"sparkdream/x/vote/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ProposalTally(ctx context.Context, req *types.QueryProposalTallyRequest) (*types.QueryProposalTallyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	proposal, err := q.k.VotingProposal.Get(ctx, req.ProposalId)
	if err != nil {
		return nil, status.Error(codes.NotFound, "proposal not found")
	}

	var totalVotes uint64
	for _, t := range proposal.Tally {
		totalVotes += t.VoteCount
	}

	return &types.QueryProposalTallyResponse{
		Tally:          proposal.Tally,
		TotalVotes:     totalVotes,
		EligibleVoters: proposal.EligibleVoters,
	}, nil
}
