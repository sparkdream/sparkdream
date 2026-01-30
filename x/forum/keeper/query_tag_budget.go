package keeper

import (
	"context"
	"errors"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListTagBudget(ctx context.Context, req *types.QueryAllTagBudgetRequest) (*types.QueryAllTagBudgetResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	tagBudgets, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.TagBudget,
		req.Pagination,
		func(_ uint64, value types.TagBudget) (types.TagBudget, error) {
			return value, nil
		},
	)

	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllTagBudgetResponse{TagBudget: tagBudgets, Pagination: pageRes}, nil
}

func (q queryServer) GetTagBudget(ctx context.Context, req *types.QueryGetTagBudgetRequest) (*types.QueryGetTagBudgetResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	tagBudget, err := q.k.TagBudget.Get(ctx, req.Id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, sdkerrors.ErrKeyNotFound
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetTagBudgetResponse{TagBudget: tagBudget}, nil
}
