package keeper

import (
	"context"
	"errors"

	"sparkdream/x/vote/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListUsedNullifier(ctx context.Context, req *types.QueryAllUsedNullifierRequest) (*types.QueryAllUsedNullifierResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	usedNullifiers, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.UsedNullifier,
		req.Pagination,
		func(_ string, value types.UsedNullifier) (types.UsedNullifier, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllUsedNullifierResponse{UsedNullifier: usedNullifiers, Pagination: pageRes}, nil
}

func (q queryServer) GetUsedNullifier(ctx context.Context, req *types.QueryGetUsedNullifierRequest) (*types.QueryGetUsedNullifierResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.UsedNullifier.Get(ctx, req.Index)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetUsedNullifierResponse{UsedNullifier: val}, nil
}
