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
	// Names stay indexed under the address they were written with. The page
	// is taken over the member's canonical address and, on a migrated
	// member, every older alias's full list is appended after it (the
	// collection paginator cannot span prefixes; alias lists are small).
	aliases := q.k.allAddressesOf(ctx, req.Address)
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
		query.WithCollectionPaginationPairPrefix[string, string](aliases[0]),
	)

	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	for _, alias := range aliases[1:] {
		err := q.k.OwnerNames.Walk(ctx,
			collections.NewPrefixedPairRange[string, string](alias),
			func(key collections.Pair[string, string]) (bool, error) {
				record, err := q.k.Names.Get(ctx, key.K2())
				if err != nil {
					return false, nil
				}
				names = append(names, record)
				return false, nil
			},
		)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	return &types.QueryNamesResponse{Names: names, Pagination: pageRes}, nil
}
