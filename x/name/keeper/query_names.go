package keeper

import (
	"context"

	"sparkdream/x/name/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) Names(ctx context.Context, req *types.QueryNamesRequest) (*types.QueryNamesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address cannot be empty")
	}

	// Iterate over the OwnerNames secondary index using the address as a prefix.
	// The Key is a Pair[OwnerAddress, Name].
	names, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.OwnerNames,
		req.Pagination,
		func(key collections.Pair[string, string], _ collections.NoValue) (types.NameRecord, error) {
			// 1. Extract the name from the key (Index 2 of the Pair)
			name := key.K2()

			// 2. Fetch the full record from the primary store
			record, err := q.k.Names.Get(ctx, name)
			if err != nil {
				return types.NameRecord{}, err
			}
			return record, nil
		},
		// Optimization: Only iterate keys where the first part of the pair (Owner) matches req.Address
		query.WithCollectionPaginationPairPrefix[string, string](req.Address),
	)

	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryNamesResponse{Names: names, Pagination: pageRes}, nil
}
