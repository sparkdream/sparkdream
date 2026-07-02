package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/collect/types"

	query "github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) PublicCollectionsByType(ctx context.Context, req *types.QueryPublicCollectionsByTypeRequest) (*types.QueryPublicCollectionsByTypeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	pageReq := req.Pagination
	if pageReq == nil {
		pageReq = &query.PageRequest{Limit: 100}
	}
	limit := pageReq.Limit
	if limit == 0 || limit > maxPublicCollectionsLimit {
		limit = maxPublicCollectionsLimit
	}
	offset := pageReq.Offset
	collType := types.CollectionType(req.CollectionType)

	// CollectionsByStatus is keyed (status, pinned-rank, id), so a status-prefixed
	// walk yields pinned-first results in ID order natively — no in-memory sort.
	// Total still requires walking the full ACTIVE+PUBLIC set for this type
	// (offset-based, not keyset, pagination).
	var matched []types.Collection
	activeStatus := int32(types.CollectionStatus_COLLECTION_STATUS_ACTIVE)
	err := q.k.CollectionsByStatus.Walk(ctx,
		collections.NewPrefixedTripleRange[int32, int32, uint64](activeStatus),
		func(key collections.Triple[int32, int32, uint64]) (bool, error) {
			coll, err := q.k.Collection.Get(ctx, key.K3())
			if err != nil {
				return false, nil
			}
			// Filter: PUBLIC visibility and matching type
			if coll.Visibility != types.Visibility_VISIBILITY_PUBLIC {
				return false, nil
			}
			if coll.Type != collType {
				return false, nil
			}
			matched = append(matched, coll)
			return false, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	total := uint64(len(matched))
	results := paginate(matched, offset, limit)

	return &types.QueryPublicCollectionsByTypeResponse{
		Collections: results,
		Pagination:  &query.PageResponse{Total: total},
	}, nil
}
