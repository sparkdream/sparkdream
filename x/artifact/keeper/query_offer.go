package keeper

import (
	"context"

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
	return &types.QueryOfferResponse{Offer: o}, nil
}
