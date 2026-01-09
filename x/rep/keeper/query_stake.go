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

func (q queryServer) ListStake(ctx context.Context, req *types.QueryAllStakeRequest) (*types.QueryAllStakeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	stakes, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Stake,
		req.Pagination,
		func(_ uint64, value types.Stake) (types.Stake, error) {
			return value, nil
		},
	)

	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllStakeResponse{Stake: stakes, Pagination: pageRes}, nil
}

func (q queryServer) GetStake(ctx context.Context, req *types.QueryGetStakeRequest) (*types.QueryGetStakeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	stake, err := q.k.Stake.Get(ctx, req.Id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, sdkerrors.ErrKeyNotFound
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetStakeResponse{Stake: stake}, nil
}
