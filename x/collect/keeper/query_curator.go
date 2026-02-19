package keeper

import (
	"context"

	"sparkdream/x/collect/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) Curator(ctx context.Context, req *types.QueryCuratorRequest) (*types.QueryCuratorResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	curator, err := q.k.Curator.Get(ctx, req.Address)
	if err != nil {
		return nil, status.Error(codes.NotFound, types.ErrNotCurator.Error())
	}

	return &types.QueryCuratorResponse{Curator: curator}, nil
}
