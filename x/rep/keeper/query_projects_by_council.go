package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ProjectsByCouncil(ctx context.Context, req *types.QueryProjectsByCouncilRequest) (*types.QueryProjectsByCouncilResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// TODO: Process the query

	return &types.QueryProjectsByCouncilResponse{}, nil
}
