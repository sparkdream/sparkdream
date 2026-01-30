package keeper

import (
	"context"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) GuildsList(ctx context.Context, req *types.QueryGuildsListRequest) (*types.QueryGuildsListResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// TODO: Process the query

	return &types.QueryGuildsListResponse{}, nil
}
