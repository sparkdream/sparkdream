package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/types"
)

func (q queryServer) GetSentinelActivity(ctx context.Context, req *types.QueryGetSentinelActivityRequest) (*types.QueryGetSentinelActivityResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	val, err := q.k.SentinelActivity.Get(ctx, req.Address)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}
		return nil, status.Error(codes.Internal, "internal error")
	}
	return &types.QueryGetSentinelActivityResponse{SentinelActivity: val}, nil
}

func (q queryServer) ListSentinelActivity(ctx context.Context, req *types.QueryAllSentinelActivityRequest) (*types.QueryAllSentinelActivityResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	items, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.SentinelActivity,
		req.Pagination,
		func(_ string, value types.SentinelActivity) (types.SentinelActivity, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryAllSentinelActivityResponse{SentinelActivity: items, Pagination: pageRes}, nil
}
