package keeper

import (
	"context"
	"strings"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) MemberByDisplayName(ctx context.Context, req *types.QueryMemberByDisplayNameRequest) (*types.QueryMemberByDisplayNameResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.DisplayName == "" {
		return nil, status.Error(codes.InvalidArgument, "display_name required")
	}

	// Normalize the display name for comparison
	normalizedName := strings.ToLower(req.DisplayName)

	// Iterate through member profiles to find one with matching display name
	iter, err := q.k.MemberProfile.Iterate(ctx, nil)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		profile, err := iter.Value()
		if err != nil {
			continue
		}
		if strings.ToLower(profile.DisplayName) == normalizedName {
			memberAddr, _ := iter.Key()
			return &types.QueryMemberByDisplayNameResponse{
				Address:  memberAddr,
				Username: profile.Username,
				SeasonXp: profile.SeasonXp,
			}, nil
		}
	}

	return nil, status.Errorf(codes.NotFound, "member with display name %s not found", req.DisplayName)
}
