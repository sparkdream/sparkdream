package keeper

import (
	"context"
	"errors"

	"sparkdream/x/split/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListShare(ctx context.Context, req *types.QueryAllShareRequest) (*types.QueryAllShareResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	shares, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Share,
		req.Pagination,
		func(_ string, value types.Share) (types.Share, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllShareResponse{Share: shares, Pagination: pageRes}, nil
}

func (q queryServer) GetShare(ctx context.Context, req *types.QueryGetShareRequest) (*types.QueryGetShareResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.Share.Get(ctx, req.Address)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetShareResponse{Share: val}, nil
}
