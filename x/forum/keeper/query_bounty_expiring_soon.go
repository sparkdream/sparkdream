package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// BountyExpiringSoon lists the ACTIVE bounties that expire within
// within_seconds, soonest first. It walks BountiesByExpiry only up to the
// threshold; next_key is the encoded index key of the first bounty of the
// next page. The legacy single-bounty fields describe the first bounty.
func (q queryServer) BountyExpiringSoon(ctx context.Context, req *types.QueryBountyExpiringSoonRequest) (*types.QueryBountyExpiringSoonResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	expirationThreshold := now + req.WithinSeconds

	limit := uint64(query.DefaultLimit)
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndInclusive(collections.Join(expirationThreshold, ^uint64(0)))
	codec := q.k.BountiesByExpiry.KeyCodec()
	if req.Pagination != nil {
		if req.Pagination.Offset != 0 {
			return nil, status.Error(codes.InvalidArgument, "offset pagination is not supported; use key")
		}
		if req.Pagination.Limit != 0 {
			limit = req.Pagination.Limit
		}
		if len(req.Pagination.Key) > 0 {
			_, start, err := codec.Decode(req.Pagination.Key)
			if err != nil {
				return nil, status.Error(codes.InvalidArgument, "invalid pagination key")
			}
			rng = rng.StartInclusive(start)
		}
	}

	var bounties []types.Bounty
	var nextKey []byte
	err := q.k.BountiesByExpiry.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		b, getErr := q.k.Bounty.Get(ctx, key.K2())
		if getErr != nil || b.Status != types.BountyStatus_BOUNTY_STATUS_ACTIVE {
			return false, nil
		}
		if uint64(len(bounties)) >= limit {
			buf := make([]byte, codec.Size(key))
			if _, err := codec.Encode(buf, key); err != nil {
				return true, err
			}
			nextKey = buf
			return true, nil
		}
		bounties = append(bounties, b)
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	resp := &types.QueryBountyExpiringSoonResponse{
		Bounties:   bounties,
		Pagination: &query.PageResponse{NextKey: nextKey},
	}
	if len(bounties) > 0 {
		resp.BountyId = bounties[0].Id
		resp.ThreadId = bounties[0].ThreadId
		resp.ExpiresAt = bounties[0].ExpiresAt
	}
	return resp, nil
}
