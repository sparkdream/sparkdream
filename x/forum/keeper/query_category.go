package keeper

import (
	"context"
	"errors"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListCategory(ctx context.Context, req *types.QueryAllCategoryRequest) (*types.QueryAllCategoryResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	categorys, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Category,
		req.Pagination,
		func(_ uint64, value types.Category) (types.Category, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllCategoryResponse{Category: categorys, Pagination: pageRes}, nil
}

func (q queryServer) GetCategory(ctx context.Context, req *types.QueryGetCategoryRequest) (*types.QueryGetCategoryResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.Category.Get(ctx, req.CategoryId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetCategoryResponse{Category: val}, nil
}
