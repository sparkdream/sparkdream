package keeper

import (
	"context"

	"sparkdream/x/collect/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) IsCollectNullifierUsed(ctx context.Context, req *types.QueryIsCollectNullifierUsedRequest) (*types.QueryIsCollectNullifierUsedResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	used := q.k.IsNullifierUsed(ctx, req.Domain, req.Scope, req.NullifierHex)

	return &types.QueryIsCollectNullifierUsedResponse{Used: used}, nil
}
