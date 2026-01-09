package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) InitiativesByProject(ctx context.Context, req *types.QueryInitiativesByProjectRequest) (*types.QueryInitiativesByProjectResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// TODO: Process the query

	return &types.QueryInitiativesByProjectResponse{}, nil
}
