package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ThreadLockStatus(ctx context.Context, req *types.QueryThreadLockStatusRequest) (*types.QueryThreadLockStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// TODO: Process the query

	return &types.QueryThreadLockStatusResponse{}, nil
}
