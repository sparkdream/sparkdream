package keeper

import (
	"context"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) MemberAchievements(ctx context.Context, req *types.QueryMemberAchievementsRequest) (*types.QueryMemberAchievementsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "member address required")
	}

	// Get member's profile to access their achievements
	profile, err := q.k.MemberProfile.Get(ctx, req.Address)
	if err != nil {
		return &types.QueryMemberAchievementsResponse{}, nil
	}

	// Return first achievement if available
	if len(profile.Achievements) == 0 {
		return &types.QueryMemberAchievementsResponse{}, nil
	}

	firstAchievementId := profile.Achievements[0]

	return &types.QueryMemberAchievementsResponse{
		AchievementId: firstAchievementId,
	}, nil
}
