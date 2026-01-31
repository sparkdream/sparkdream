package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/types/query"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) Achievements(ctx context.Context, req *types.QueryAchievementsRequest) (*types.QueryAchievementsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Use collection query for pagination
	achievements, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Achievement,
		req.Pagination,
		func(key string, achievement types.Achievement) (types.Achievement, error) {
			return achievement, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if len(achievements) == 0 {
		return &types.QueryAchievementsResponse{
			Pagination: pageRes,
		}, nil
	}

	firstAchievement := achievements[0]
	return &types.QueryAchievementsResponse{
		Id:         firstAchievement.AchievementId,
		Name:       firstAchievement.Name,
		Rarity:     uint64(firstAchievement.Rarity),
		XpReward:   firstAchievement.XpReward,
		Pagination: pageRes,
	}, nil
}
