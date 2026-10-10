package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/artifact/types"
)

func (q queryServer) Listing(ctx context.Context, req *types.QueryListingRequest) (*types.QueryListingResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	l, err := q.k.Listings.Get(ctx, collections.Join(req.ClassId, req.TokenId))
	if err != nil {
		return nil, notFound(err, "listing")
	}
	if l.ExpiresAt <= now(ctx) {
		return nil, notFound(collections.ErrNotFound, "listing")
	}
	return &types.QueryListingResponse{Listing: l}, nil
}
