package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) IsFollowingThread(ctx context.Context, req *types.QueryIsFollowingThreadRequest) (*types.QueryIsFollowingThreadResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// TODO: Process the query

	return &types.QueryIsFollowingThreadResponse{}, nil
}
