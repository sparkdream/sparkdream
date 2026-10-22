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
	return &types.QueryRentalResponse{Rental: r}, nil
}
