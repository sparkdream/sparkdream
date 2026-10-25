package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/collect/types"

	query "github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ItemsByOwner(ctx context.Context, req *types.QueryItemsByOwnerRequest) (*types.QueryItemsByOwnerResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var results []types.Item
	pageReq := req.Pagination
	if pageReq == nil {
		pageReq = &query.PageRequest{Limit: 100}
	}
	limit := pageReq.Limit
	if limit == 0 {
		limit = 100
	}
	offset := pageReq.Offset
	var count uint64

	// Items stay indexed under the address that added them; the offset/limit
	// window runs across every alias of a migrated member in sequence
	// (canonical address first).
	for _, owner := range q.k.allAddressesOf(ctx, req.Owner) {
		err := q.k.ItemsByOwner.Walk(ctx,
			collections.NewPrefixedPairRange[string, uint64](owner),
			func(key collections.Pair[string, uint64]) (bool, error) {
				if count < offset {
					count++
					return false, nil
				}
				if uint64(len(results)) >= limit {
					return true, nil
				}
				item, err := q.k.Item.Get(ctx, key.K2())
				if err != nil {
					count++
					return false, nil
				}
				results = append(results, item)
				count++
				return false, nil
			},
		)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	return &types.QueryItemsByOwnerResponse{
		Items:      results,
		Pagination: &query.PageResponse{Total: count},
	}, nil
}
