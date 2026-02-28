package keeper

import (
	"context"

	"sparkdream/x/collect/types"

	"cosmossdk.io/collections"
	query "github.com/cosmos/cosmos-sdk/types/query"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) AnonymousCollections(ctx context.Context, req *types.QueryAnonymousCollectionsRequest) (*types.QueryAnonymousCollectionsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Anonymous collections are owned by the collect module account (not the governance authority)
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName).String()

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

	err := q.k.CollectionsByOwner.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](moduleAddr),
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

	return &types.QueryAnonymousCollectionsResponse{
		Collections: results,
		Pagination:  &query.PageResponse{Total: count},
	}, nil
}
