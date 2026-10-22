package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/artifact/types"
)

func (q queryServer) Balance(ctx context.Context, req *types.QueryBalanceRequest) (*types.QueryBalanceResponse, error) {
	if req == nil || req.Owner == "" {
		return nil, errInvalidRequest
	}
	e, err := q.k.Editions.Get(ctx, collections.Join(req.ClassId, req.EditionId))
	if err != nil {
		return nil, notFound(err, "edition")
	}
	b, err := q.k.Balances.Get(ctx, balanceKey(req.ClassId, req.EditionId, req.Owner))
	if err != nil {
		return nil, notFound(err, "balance")
	}
	return &types.QueryBalanceResponse{Balance: balanceView(e, b)}, nil
}
