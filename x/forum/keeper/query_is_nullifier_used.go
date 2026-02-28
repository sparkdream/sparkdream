package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) IsNullifierUsed(ctx context.Context, req *types.QueryIsNullifierUsedRequest) (*types.QueryIsNullifierUsedResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	used := q.k.IsNullifierUsed(ctx, req.Domain, req.Scope, req.NullifierHex)

	return &types.QueryIsNullifierUsedResponse{Used: used}, nil
}
