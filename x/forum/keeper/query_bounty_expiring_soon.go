package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
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

	// FORUM-S2-8: walk BountiesByExpiry ascending up to the threshold instead
	// of scanning every bounty in the store.
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndInclusive(collections.Join(expirationThreshold, ^uint64(0)))

	var resp types.QueryBountyExpiringSoonResponse
	found := false

	err := q.k.BountiesByExpiry.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		bid := key.K2()
		b, getErr := q.k.Bounty.Get(ctx, bid)
		if getErr != nil {
			return false, nil
		}
		if b.Status != types.BountyStatus_BOUNTY_STATUS_ACTIVE {
			return false, nil
		}
		resp.BountyId = b.Id
		resp.ThreadId = b.ThreadId
		resp.ExpiresAt = b.ExpiresAt
		found = true
		return true, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if !found {
		return &types.QueryBountyExpiringSoonResponse{}, nil
	}
	return &resp, nil
}
