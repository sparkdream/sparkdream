package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
)

// UnitListings lists an edition's active unit listings (expired listings
// the EndBlocker has not drained are filtered).
func (q queryServer) UnitListings(ctx context.Context, req *types.QueryUnitListingsRequest) (*types.QueryUnitListingsResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	t := now(ctx)
	listings, page, err := query.CollectionFilteredPaginate(ctx, q.k.UnitListingsByEdition, capPage(req.Pagination),
		func(key collections.Triple[uint64, uint64, uint64], _ collections.NoValue) (bool, error) {
			l, err := q.k.UnitListings.Get(ctx, key.K3())
			return err == nil && l.ExpiresAt > t, nil
		},
		func(key collections.Triple[uint64, uint64, uint64], _ collections.NoValue) (types.UnitListing, error) {
			return q.k.UnitListings.Get(ctx, key.K3())
		}, editionPrefix[uint64](req.ClassId, req.EditionId))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryUnitListingsResponse{Listings: listings, Pagination: page}, nil
}
