package keeper

import (
	"context"

	"sparkdream/x/artifact/types"
)

func (q queryServer) Supply(ctx context.Context, req *types.QuerySupplyRequest) (*types.QuerySupplyResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	c, err := q.k.Classes.Get(ctx, req.ClassId)
	if err != nil {
		return nil, notFound(err, "class")
	}
	return &types.QuerySupplyResponse{
		Supply:         c.Supply,
		Burned:         c.Burned,
		ReservedSupply: c.ReservedSupply,
		MaxSupply:      c.MaxSupply,
		MintingClosed:  c.MintingClosed,
	}, nil
}
