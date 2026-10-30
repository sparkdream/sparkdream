package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/artifact/types"
)

func (q queryServer) Offer(ctx context.Context, req *types.QueryOfferRequest) (*types.QueryOfferResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	o, err := q.k.Offers.Get(ctx, req.OfferId)
	if err != nil {
		return nil, notFound(err, "offer")
	}
	// An expired offer the EndBlocker has not refunded yet is filtered (§6).
	if o.ExpiresAt <= now(ctx) {
		return nil, notFound(collections.ErrNotFound, "offer")
	}
	return &types.QueryOfferResponse{Offer: o}, nil
}
