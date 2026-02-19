package keeper

import (
	"context"

	"sparkdream/x/collect/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) Collection(ctx context.Context, req *types.QueryCollectionRequest) (*types.QueryCollectionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	coll, err := q.k.Collection.Get(ctx, req.Id)
	if err != nil {
		return nil, status.Error(codes.NotFound, types.ErrCollectionNotFound.Error())
	}

	return &types.QueryCollectionResponse{Collection: coll}, nil
}
