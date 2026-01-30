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

func (q queryServer) ListUserReactionLimit(ctx context.Context, req *types.QueryAllUserReactionLimitRequest) (*types.QueryAllUserReactionLimitResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	userReactionLimits, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.UserReactionLimit,
		req.Pagination,
		func(_ string, value types.UserReactionLimit) (types.UserReactionLimit, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllUserReactionLimitResponse{UserReactionLimit: userReactionLimits, Pagination: pageRes}, nil
}

func (q queryServer) GetUserReactionLimit(ctx context.Context, req *types.QueryGetUserReactionLimitRequest) (*types.QueryGetUserReactionLimitResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.UserReactionLimit.Get(ctx, req.UserAddress)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetUserReactionLimitResponse{UserReactionLimit: val}, nil
}
