package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/types"
)

func (q queryServer) TagExists(ctx context.Context, req *types.QueryTagExistsRequest) (*types.QueryTagExistsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.TagName == "" {
		return nil, status.Error(codes.InvalidArgument, "tag name required")
	}

	tag, err := q.k.Tag.Get(ctx, req.TagName)
	if err != nil {
		return &types.QueryTagExistsResponse{
			Exists:         false,
			ExpirationTime: 0,
		}, nil
	}

	return &types.QueryTagExistsResponse{
		Exists:         true,
		ExpirationTime: tag.ExpirationIndex,
	}, nil
}
