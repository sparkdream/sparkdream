package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
)

// RentalOffer returns a token's open rental offer (not-found once expired).
func (q queryServer) RentalOffer(ctx context.Context, req *types.QueryRentalOfferRequest) (*types.QueryRentalOfferResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	o, err := q.k.RentalOffers.Get(ctx, collections.Join(req.ClassId, req.TokenId))
	if err != nil {
		return nil, notFound(err, "rental offer")
	}
	if o.ExpiresAt <= now(ctx) {
		return nil, status.Error(codes.NotFound, "rental offer not found")
	}
	return &types.QueryRentalOfferResponse{Offer: o}, nil
}
