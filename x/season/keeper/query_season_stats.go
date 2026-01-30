package keeper

import (
	"context"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) SeasonStats(ctx context.Context, req *types.QuerySeasonStatsRequest) (*types.QuerySeasonStatsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// TODO: Process the query

	return &types.QuerySeasonStatsResponse{}, nil
}
