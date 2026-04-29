package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
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

	// FORUM-S2-8: prefix-walk BountiesByCreator instead of scanning all bounties.
	rng := collections.NewPrefixedPairRange[string, uint64](req.User)

	var resp types.QueryUserBountiesResponse
	found := false

	err := q.k.BountiesByCreator.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
		bid := key.K2()
		b, getErr := q.k.Bounty.Get(ctx, bid)
		if getErr != nil {
			return false, nil
		}
		resp.BountyId = b.Id
		resp.ThreadId = b.ThreadId
		resp.Status = uint64(b.Status)
		found = true
		return true, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if !found {
		return &types.QueryUserBountiesResponse{}, nil
	}
	return &resp, nil
}
