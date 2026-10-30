package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/artifact/types"
)

func (q queryServer) UnitListing(ctx context.Context, req *types.QueryUnitListingRequest) (*types.QueryUnitListingResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	l, err := q.k.UnitListings.Get(ctx, req.ListingId)
	if err != nil {
		return nil, notFound(err, "unit listing")
	}
	if l.ExpiresAt <= now(ctx) {
		return nil, notFound(collections.ErrNotFound, "unit listing")
	}
	return &types.QueryUnitListingResponse{Listing: l}, nil
}
