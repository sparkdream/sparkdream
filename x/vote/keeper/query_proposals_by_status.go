package keeper

import (
	"context"

	"sparkdream/x/vote/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ProposalsByStatus(ctx context.Context, req *types.QueryProposalsByStatusRequest) (*types.QueryProposalsByStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	targetStatus := types.ProposalStatus(req.Status)
	var proposals []types.VotingProposal

	err := q.k.VotingProposal.Walk(ctx, nil, func(_ uint64, p types.VotingProposal) (bool, error) {
		if p.Status == targetStatus {
			proposals = append(proposals, p)
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryProposalsByStatusResponse{
		Proposals: proposals,
	}, nil
}
