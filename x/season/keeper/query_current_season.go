package keeper

import (
	"context"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) CurrentSeason(ctx context.Context, req *types.QueryCurrentSeasonRequest) (*types.QueryCurrentSeasonResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// TODO: Process the query

	return &types.QueryCurrentSeasonResponse{}, nil
}
