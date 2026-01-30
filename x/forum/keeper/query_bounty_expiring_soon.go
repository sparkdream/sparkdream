package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) BountyExpiringSoon(ctx context.Context, req *types.QueryBountyExpiringSoonRequest) (*types.QueryBountyExpiringSoonResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// TODO: Process the query

	return &types.QueryBountyExpiringSoonResponse{}, nil
}
