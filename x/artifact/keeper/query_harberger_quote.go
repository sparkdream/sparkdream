package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
)

func (q queryServer) HarbergerQuote(ctx context.Context, req *types.QueryHarbergerQuoteRequest) (*types.QueryHarbergerQuoteResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	c, err := q.k.Classes.Get(ctx, req.ClassId)
	if err != nil {
		return nil, notFound(err, "class")
	}
	if !c.IsHarberger() {
		return nil, status.Error(codes.InvalidArgument, "not a harberger class")
	}
	t, err := q.k.Tokens.Get(ctx, collections.Join(req.ClassId, req.TokenId))
	if err != nil {
		return nil, notFound(err, "token")
	}
	state := types.HarbergerState{Price: math.ZeroInt(), TaxPaid: math.ZeroInt()}
	if t.Harberger != nil {
		state = *t.Harberger
	}
	price, foreclosed := types.CurrentHarbergerPrice(c.Harberger, state, now(ctx))
	return &types.QueryHarbergerQuoteResponse{
		State:        state,
		CurrentPrice: price.String(),
		TaxPerPeriod: c.Harberger.TaxPerPeriod(state.Price).String(),
		Foreclosed:   foreclosed,
		ForSale:      types.HarbergerForSale(state, now(ctx)) && !isTargetHidden(c, t),
	}, nil
}
