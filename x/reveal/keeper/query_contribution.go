package keeper

import (
	"context"

	"sparkdream/x/reveal/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) Contribution(ctx context.Context, req *types.QueryContributionRequest) (*types.QueryContributionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	contrib, err := q.k.Contribution.Get(ctx, req.ContributionId)
	if err != nil {
		return nil, status.Error(codes.NotFound, types.ErrContributionNotFound.Error())
	}

	return &types.QueryContributionResponse{Contribution: contrib}, nil
}
