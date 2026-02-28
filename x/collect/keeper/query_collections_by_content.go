package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"

	"sparkdream/x/collect/types"

	query "github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) CollectionsByContent(ctx context.Context, req *types.QueryCollectionsByContentRequest) (*types.QueryCollectionsByContentResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.Module == "" || req.EntityType == "" || req.EntityId == "" {
		return nil, status.Error(codes.InvalidArgument, "module, entity_type, and entity_id are required")
	}

	refKey := fmt.Sprintf("%s:%s:%s", req.Module, req.EntityType, req.EntityId)

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

	// Track seen collection IDs to deduplicate (multiple items in the same
	// collection can reference the same content).
	seen := make(map[uint64]bool)
	var count uint64

	err := q.k.ItemsByOnChainRef.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](refKey),
		func(key collections.Pair[string, uint64]) (bool, error) {
			itemID := key.K2()
			item, err := q.k.Item.Get(ctx, itemID)
			if err != nil {
				// Item was deleted but index entry remains — skip
				return false, nil
			}

			collID := item.CollectionId
			if seen[collID] {
				return false, nil
			}
			seen[collID] = true

			count++
			if count <= offset {
				return false, nil
			}
			if uint64(len(results)) >= limit {
				return true, nil
			}

			coll, err := q.k.Collection.Get(ctx, collID)
			if err != nil {
				// Collection was deleted — skip
				return false, nil
			}
			results = append(results, coll)
			return false, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryCollectionsByContentResponse{
		Collections: results,
		Pagination:  &query.PageResponse{Total: count},
	}, nil
}
