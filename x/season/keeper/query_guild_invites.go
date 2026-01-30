package keeper

import (
	"context"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) GuildInvites(ctx context.Context, req *types.QueryGuildInvitesRequest) (*types.QueryGuildInvitesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// TODO: Process the query

	return &types.QueryGuildInvitesResponse{}, nil
}
