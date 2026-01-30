package keeper

import (
	"context"
	"errors"

	"sparkdream/x/season/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListDisplayNameModeration(ctx context.Context, req *types.QueryAllDisplayNameModerationRequest) (*types.QueryAllDisplayNameModerationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	displayNameModerations, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.DisplayNameModeration,
		req.Pagination,
		func(_ string, value types.DisplayNameModeration) (types.DisplayNameModeration, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllDisplayNameModerationResponse{DisplayNameModeration: displayNameModerations, Pagination: pageRes}, nil
}

func (q queryServer) GetDisplayNameModeration(ctx context.Context, req *types.QueryGetDisplayNameModerationRequest) (*types.QueryGetDisplayNameModerationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.DisplayNameModeration.Get(ctx, req.Member)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetDisplayNameModerationResponse{DisplayNameModeration: val}, nil
}
