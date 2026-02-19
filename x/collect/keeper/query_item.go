package keeper

import (
	"context"

	"sparkdream/x/collect/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) Item(ctx context.Context, req *types.QueryItemRequest) (*types.QueryItemResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	item, err := q.k.Item.Get(ctx, req.Id)
	if err != nil {
		return nil, status.Error(codes.NotFound, types.ErrItemNotFound.Error())
	}

	return &types.QueryItemResponse{Item: item}, nil
}
