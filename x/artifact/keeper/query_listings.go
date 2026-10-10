package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
)

func (q queryServer) Listings(ctx context.Context, req *types.QueryListingsRequest) (*types.QueryListingsResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	t := now(ctx)
	var opts []func(o *query.CollectionsPaginateOptions[U64Pair])
	if req.ClassId != 0 {
		opts = append(opts, query.WithCollectionPaginationPairPrefix[uint64, uint64](req.ClassId))
	}
	listings, page, err := query.CollectionFilteredPaginate(ctx, q.k.Listings, capPage(req.Pagination),
		func(_ U64Pair, l types.Listing) (bool, error) { return l.ExpiresAt > t, nil },
		func(_ U64Pair, l types.Listing) (types.Listing, error) { return l, nil },
		opts...)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryListingsResponse{Listings: listings, Pagination: page}, nil
}
