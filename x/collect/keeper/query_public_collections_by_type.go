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

	var results []types.Collection
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
	collType := types.CollectionType(req.CollectionType)

	// Walk CollectionsByStatus index for ACTIVE status
	activeStatus := int32(types.CollectionStatus_COLLECTION_STATUS_ACTIVE)
	err := q.k.CollectionsByStatus.Walk(ctx,
		collections.NewPrefixedPairRange[int32, uint64](activeStatus),
		func(key collections.Pair[int32, uint64]) (bool, error) {
			coll, err := q.k.Collection.Get(ctx, key.K2())
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
			if count < offset {
				count++
				return false, nil
			}
			if uint64(len(results)) >= limit {
				return true, nil
			}
			results = append(results, coll)
			count++
			return false, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryPublicCollectionsByTypeResponse{
		Collections: results,
		Pagination:  &query.PageResponse{Total: count},
	}, nil
}
