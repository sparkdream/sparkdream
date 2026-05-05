package keeper

import (
	"context"

	"sparkdream/x/name/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Targets returns the list of NameRecord entries where the requested address
// is the accepted resolver target. These names are eligible to be set as the
// address's primary for reverse resolution.
func (q queryServer) Targets(ctx context.Context, req *types.QueryTargetsRequest) (*types.QueryTargetsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address cannot be empty")
	}

	names, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.AcceptedTargets,
		req.Pagination,
		func(key collections.Pair[string, string], _ collections.NoValue) (types.NameRecord, error) {
			name := key.K2()
			record, err := q.k.Names.Get(ctx, name)
			if err != nil {
				return types.NameRecord{}, err
			}
			return record, nil
		},
		query.WithCollectionPaginationPairPrefix[string, string](req.Address),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryTargetsResponse{Names: names, Pagination: pageRes}, nil
}
