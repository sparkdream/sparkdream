package keeper

import (
	"context"

	"sparkdream/x/vote/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) Proposal(ctx context.Context, req *types.QueryProposalRequest) (*types.QueryProposalResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	proposal, err := q.k.VotingProposal.Get(ctx, req.ProposalId)
	if err != nil {
		return nil, status.Error(codes.NotFound, "proposal not found")
	}

	return &types.QueryProposalResponse{Proposal: proposal}, nil
}
