package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ActiveBounties(ctx context.Context, req *types.QueryActiveBountiesRequest) (*types.QueryActiveBountiesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// TODO: Process the query

	return &types.QueryActiveBountiesResponse{}, nil
}
