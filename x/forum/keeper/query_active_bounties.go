package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ActiveBounties lists ACTIVE bounties, soonest expiry first, paginated over
// the BountiesByExpiry index (which holds ACTIVE bounties only). The legacy
// single-bounty fields describe the first bounty of the page.
func (q queryServer) ActiveBounties(ctx context.Context, req *types.QueryActiveBountiesRequest) (*types.QueryActiveBountiesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	bounties, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		q.k.BountiesByExpiry,
		req.Pagination,
		func(key collections.Pair[int64, uint64], _ collections.NoValue) (bool, error) {
			b, err := q.k.Bounty.Get(ctx, key.K2())
			return err == nil && b.Status == types.BountyStatus_BOUNTY_STATUS_ACTIVE, nil
		},
		func(key collections.Pair[int64, uint64], _ collections.NoValue) (types.Bounty, error) {
			return q.k.Bounty.Get(ctx, key.K2())
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	resp := &types.QueryActiveBountiesResponse{Bounties: bounties, Pagination: pageRes}
	if len(bounties) > 0 {
		resp.BountyId = bounties[0].Id
		resp.ThreadId = bounties[0].ThreadId
		resp.Amount = bounties[0].Amount
	}
	return resp, nil
}
