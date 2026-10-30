package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/artifact/types"
)

func (q queryServer) Rental(ctx context.Context, req *types.QueryRentalRequest) (*types.QueryRentalResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	r, err := q.k.Rentals.Get(ctx, collections.Join(req.ClassId, req.TokenId))
	if err != nil {
		return nil, notFound(err, "rental")
	}
	// A rental past its term the EndBlocker has not ended yet is filtered
	// (§6): the renter's right of use is over.
	if r.ExpiresAt <= now(ctx) {
		return nil, notFound(collections.ErrNotFound, "rental")
	}
	return &types.QueryRentalResponse{Rental: r}, nil
}
