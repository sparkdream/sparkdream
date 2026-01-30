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

func (q queryServer) ListUserRateLimit(ctx context.Context, req *types.QueryAllUserRateLimitRequest) (*types.QueryAllUserRateLimitResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	userRateLimits, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.UserRateLimit,
		req.Pagination,
		func(_ string, value types.UserRateLimit) (types.UserRateLimit, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllUserRateLimitResponse{UserRateLimit: userRateLimits, Pagination: pageRes}, nil
}

func (q queryServer) GetUserRateLimit(ctx context.Context, req *types.QueryGetUserRateLimitRequest) (*types.QueryGetUserRateLimitResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.UserRateLimit.Get(ctx, req.UserAddress)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetUserRateLimitResponse{UserRateLimit: val}, nil
}
