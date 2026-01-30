package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) BountyExpiringSoon(ctx context.Context, req *types.QueryBountyExpiringSoonRequest) (*types.QueryBountyExpiringSoonResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()
	expirationThreshold := now + req.WithinSeconds

	// Find first bounty expiring within threshold (simplified - in production would return list)
	var expiringBounty *types.Bounty

	err := q.k.Bounty.Walk(ctx, nil, func(key uint64, bounty types.Bounty) (bool, error) {
		if bounty.Status == types.BountyStatus_BOUNTY_STATUS_ACTIVE &&
			bounty.ExpiresAt > 0 &&
			bounty.ExpiresAt <= expirationThreshold {
			expiringBounty = &bounty
			return true, nil // Stop after first
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if expiringBounty != nil {
		return &types.QueryBountyExpiringSoonResponse{
			BountyId:  expiringBounty.Id,
			ThreadId:  expiringBounty.ThreadId,
			ExpiresAt: expiringBounty.ExpiresAt,
		}, nil
	}

	return &types.QueryBountyExpiringSoonResponse{}, nil
}
