package keeper

import (
	"context"

	"cosmossdk.io/collections"
	query "github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/collect/types"
)

// ListCollectionsByTag returns paginated collections carrying a given tag,
// using the CollectionsByTag secondary index (maintained on Create/Update/Delete).
func (q queryServer) ListCollectionsByTag(ctx context.Context, req *types.QueryListCollectionsByTagRequest) (*types.QueryListCollectionsByTagResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.Tag == "" {
		return nil, status.Error(codes.InvalidArgument, "tag cannot be empty")
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

	err := q.k.CollectionsByTag.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](req.Tag),
		func(key collections.Pair[string, uint64]) (bool, error) {
			if count < offset {
				count++
				return false, nil
			}
			if uint64(len(results)) >= limit {
				return true, nil
			}
			coll, err := q.k.Collection.Get(ctx, key.K2())
			if err != nil {
				count++
				return false, nil
			}
			results = append(results, coll)
			count++
			return false, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryListCollectionsByTagResponse{
		Collections: results,
		Pagination:  &query.PageResponse{Total: count},
	}, nil
}
