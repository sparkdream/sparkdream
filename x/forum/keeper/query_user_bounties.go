package keeper

import (
	"context"
	"encoding/binary"
	commontypes "sparkdream/x/common/types"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UserBounties lists the bounties a member created, under the canonical
// address and every past alias (bounties created before a key migration keep
// the old creator), by prefix-walking BountiesByCreator. Pages are cut by
// position across the aliases: next_key encodes the position of the next
// bounty, and offset is accepted too. The legacy single-bounty fields
// describe the first bounty of the page.
func (q queryServer) UserBounties(ctx context.Context, req *types.QueryUserBountiesRequest) (*types.QueryUserBountiesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.User == "" {
		return nil, status.Error(codes.InvalidArgument, "user address required")
	}

	var skip uint64
	limit := uint64(query.DefaultLimit)
	countTotal := false
	if p := req.Pagination; p != nil {
		switch {
		case len(p.Key) == 8:
			skip = binary.BigEndian.Uint64(p.Key)
		case len(p.Key) != 0:
			return nil, status.Error(codes.InvalidArgument, "invalid pagination key")
		default:
			skip = p.Offset
		}
		if p.Limit != 0 {
			limit = p.Limit
		}
		countTotal = p.CountTotal
	}

	var bounties []types.Bounty
	var pos uint64
	more := false
	for _, alias := range q.k.allAddressesOf(ctx, req.User) {
		rng := collections.NewPrefixedPairRange[string, uint64](alias)
		err := q.k.BountiesByCreator.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
			b, getErr := q.k.Bounty.Get(ctx, key.K2())
			if getErr != nil {
				return false, nil
			}
			defer func() { pos++ }()
			if pos < skip {
				return false, nil
			}
			if uint64(len(bounties)) >= limit {
				more = true
				return !countTotal, nil
			}
			bounties = append(bounties, b)
			return false, nil
		})
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		if more && !countTotal {
			break
		}
	}

	pageRes := &query.PageResponse{}
	if more {
		pageRes.NextKey = binary.BigEndian.AppendUint64(nil, skip+uint64(len(bounties)))
	}
	if countTotal {
		pageRes.Total = pos
	}
	resp := &types.QueryUserBountiesResponse{Bounties: bounties, Pagination: pageRes}
	if len(bounties) > 0 {
		resp.BountyId = bounties[0].Id
		resp.ThreadId = bounties[0].ThreadId
		resp.Status = commontypes.Saturate[uint64](bounties[0].Status)
	}
	return resp, nil
}
