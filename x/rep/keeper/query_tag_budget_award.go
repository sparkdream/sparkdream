package keeper

import (
	"context"
	"errors"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListTagBudgetAward(ctx context.Context, req *types.QueryAllTagBudgetAwardRequest) (*types.QueryAllTagBudgetAwardResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	awards, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.TagBudgetAward,
		req.Pagination,
		func(_ uint64, value types.TagBudgetAward) (types.TagBudgetAward, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllTagBudgetAwardResponse{TagBudgetAward: awards, Pagination: pageRes}, nil
}

func (q queryServer) GetTagBudgetAward(ctx context.Context, req *types.QueryGetTagBudgetAwardRequest) (*types.QueryGetTagBudgetAwardResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	award, err := q.k.TagBudgetAward.Get(ctx, req.Id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, sdkerrors.ErrKeyNotFound
		}
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetTagBudgetAwardResponse{TagBudgetAward: award}, nil
}
