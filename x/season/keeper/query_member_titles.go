package keeper

import (
	"context"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) MemberTitles(ctx context.Context, req *types.QueryMemberTitlesRequest) (*types.QueryMemberTitlesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "member address required")
	}

	// Get member's profile to access their titles
	profile, err := q.k.MemberProfile.Get(ctx, req.Address)
	if err != nil {
		return &types.QueryMemberTitlesResponse{}, nil
	}

	// Return first unlocked title if available
	if len(profile.UnlockedTitles) == 0 {
		return &types.QueryMemberTitlesResponse{}, nil
	}

	firstTitleId := profile.UnlockedTitles[0]

	return &types.QueryMemberTitlesResponse{
		TitleId:    firstTitleId,
		UnlockedAt: 0, // Unlock timestamp not tracked in MemberProfile
	}, nil
}
