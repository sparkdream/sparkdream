package keeper

import (
	"context"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) GuildsByFounder(ctx context.Context, req *types.QueryGuildsByFounderRequest) (*types.QueryGuildsByFounderResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// TODO: Process the query

	return &types.QueryGuildsByFounderResponse{}, nil
}
