package keeper

import (
	"context"

	"sparkdream/x/collect/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ContentFlag(ctx context.Context, req *types.QueryContentFlagRequest) (*types.QueryContentFlagResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	key := FlagCompositeKey(req.TargetType, req.TargetId)
	flag, err := q.k.Flag.Get(ctx, key)
	if err != nil {
		return nil, status.Error(codes.NotFound, "content flag not found")
	}

	return &types.QueryContentFlagResponse{CollectionFlag: flag}, nil
}
