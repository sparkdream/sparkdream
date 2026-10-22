package keeper

import (
	"context"

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
	return &types.QueryUnitListingResponse{Listing: l}, nil
}
