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

func (q queryServer) ListAchievement(ctx context.Context, req *types.QueryAllAchievementRequest) (*types.QueryAllAchievementResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	achievements, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Achievement,
		req.Pagination,
		func(_ string, value types.Achievement) (types.Achievement, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllAchievementResponse{Achievement: achievements, Pagination: pageRes}, nil
}

func (q queryServer) GetAchievement(ctx context.Context, req *types.QueryGetAchievementRequest) (*types.QueryGetAchievementResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.Achievement.Get(ctx, req.AchievementId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetAchievementResponse{Achievement: val}, nil
}
