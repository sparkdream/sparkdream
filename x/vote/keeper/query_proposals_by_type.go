package keeper

import (
	"context"

	"sparkdream/x/vote/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ProposalsByType(ctx context.Context, req *types.QueryProposalsByTypeRequest) (*types.QueryProposalsByTypeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	targetType := types.ProposalType(req.ProposalType)
	var proposals []types.VotingProposal

	err := q.k.VotingProposal.Walk(ctx, nil, func(_ uint64, p types.VotingProposal) (bool, error) {
		if p.ProposalType == targetType {
			proposals = append(proposals, p)
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryProposalsByTypeResponse{
		Proposals: proposals,
	}, nil
}
