package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/collect/types"

	query "github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) CollectionsByCollaborator(ctx context.Context, req *types.QueryCollectionsByCollaboratorRequest) (*types.QueryCollectionsByCollaboratorResponse, error) {
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

	// Walk CollaboratorReverse index with prefix on address
	err := q.k.CollaboratorReverse.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](req.Address),
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

	return &types.QueryCollectionsByCollaboratorResponse{
		Collections: results,
		Pagination:  &query.PageResponse{Total: count},
	}, nil
}
