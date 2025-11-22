package keeper

import (
	"context"
	"errors"

	"sparkdream/x/name/types"

	"cosmossdk.io/collections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) Resolve(ctx context.Context, req *types.QueryResolveRequest) (*types.QueryResolveResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name cannot be empty")
	}

	// Fetch the name record from the store
	val, err := q.k.Names.Get(ctx, req.Name)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "name not found")
		}
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryResolveResponse{NameRecord: &val}, nil
}
