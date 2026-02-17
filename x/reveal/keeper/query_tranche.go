package keeper

import (
	"context"

	"sparkdream/x/reveal/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) Tranche(ctx context.Context, req *types.QueryTrancheRequest) (*types.QueryTrancheResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	contrib, err := q.k.Contribution.Get(ctx, req.ContributionId)
	if err != nil {
		return nil, status.Error(codes.NotFound, types.ErrContributionNotFound.Error())
	}

	tranche, err := GetTranche(&contrib, req.TrancheId)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	return &types.QueryTrancheResponse{Tranche: *tranche}, nil
}
