package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/collect/types"

	query "github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) CollectionsByOwner(ctx context.Context, req *types.QueryCollectionsByOwnerRequest) (*types.QueryCollectionsByOwnerResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	pageReq := req.Pagination
	if pageReq == nil {
		pageReq = &query.PageRequest{Limit: 100}
	}
	limit := pageReq.Limit
	if limit == 0 {
		limit = 100
	}
	offset := pageReq.Offset

	// CollectionsByOwner is keyed (owner, id), so a walk is ID-ordered. An owner's
	// collection set is naturally bounded (scoped to one address), so we collect
	// it and stable-partition pinned-first in memory rather than carrying a
	// pinned-rank in this index — see index_collections_by_status.go for why the
	// public, unbounded list earns a denormalized rank instead.
	var matched []types.Collection
	err := q.k.CollectionsByOwner.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](req.Owner),
		func(key collections.Pair[string, uint64]) (bool, error) {
			coll, err := q.k.Collection.Get(ctx, key.K2())
			if err != nil {
				// Skip entries where collection was deleted but index remains
				return false, nil
			}
			matched = append(matched, coll)
			return false, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	ordered := pinnedFirst(matched)
	total := uint64(len(ordered))
	results := paginate(ordered, offset, limit)

	return &types.QueryCollectionsByOwnerResponse{
		Collections: results,
		Pagination:  &query.PageResponse{Total: total},
	}, nil
}
