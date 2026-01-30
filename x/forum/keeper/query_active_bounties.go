package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ActiveBounties(ctx context.Context, req *types.QueryActiveBountiesRequest) (*types.QueryActiveBountiesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Find first active bounty (simplified - in production would return list with pagination)
	var activeBounty *types.Bounty

	err := q.k.Bounty.Walk(ctx, nil, func(key uint64, bounty types.Bounty) (bool, error) {
		if bounty.Status == types.BountyStatus_BOUNTY_STATUS_ACTIVE {
			activeBounty = &bounty
			return true, nil // Stop after first
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if activeBounty != nil {
		return &types.QueryActiveBountiesResponse{
			BountyId: activeBounty.Id,
			ThreadId: activeBounty.ThreadId,
			Amount:   activeBounty.Amount,
		}, nil
	}

	return &types.QueryActiveBountiesResponse{}, nil
}
