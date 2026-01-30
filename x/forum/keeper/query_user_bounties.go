package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) UserBounties(ctx context.Context, req *types.QueryUserBountiesRequest) (*types.QueryUserBountiesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.User == "" {
		return nil, status.Error(codes.InvalidArgument, "user address required")
	}

	// Find first bounty created by this user (simplified - in production would return list)
	var userBounty *types.Bounty

	err := q.k.Bounty.Walk(ctx, nil, func(key uint64, bounty types.Bounty) (bool, error) {
		if bounty.Creator == req.User {
			userBounty = &bounty
			return true, nil // Stop after first
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if userBounty != nil {
		return &types.QueryUserBountiesResponse{
			BountyId: userBounty.Id,
			ThreadId: userBounty.ThreadId,
			Status:   uint64(userBounty.Status),
		}, nil
	}

	return &types.QueryUserBountiesResponse{}, nil
}
