package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) Categories(ctx context.Context, req *types.QueryCategoriesRequest) (*types.QueryCategoriesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	categories, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Category,
		req.Pagination,
		func(_ uint64, c types.Category) (types.Category, error) {
			return c, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryCategoriesResponse{Categories: categories, Pagination: pageRes}, nil
}
