package keeper

import (
	"context"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

func (q queryServer) PublicMintQuote(ctx context.Context, req *types.QueryPublicMintQuoteRequest) (*types.QueryPublicMintQuoteResponse, error) {
	if req == nil || req.Quantity == 0 {
		return nil, errInvalidRequest
	}
	c, err := q.k.Classes.Get(ctx, req.ClassId)
	if err != nil {
		return nil, notFound(err, "class")
	}
	p := q.k.GetParams(ctx)
	denom := q.k.BondDenom(ctx)
	price := c.MintPolicy.Price.Amount
	total := price.MulRaw(int64(req.Quantity))
	fee := total.MulRaw(int64(p.SaleFeeBps)).QuoRaw(10000)
	resp := &types.QueryPublicMintQuoteResponse{
		TotalPrice:         sdk.NewCoin(denom, total),
		Fee:                sdk.NewCoin(denom, fee),
		Deposit:            sdk.NewCoin(denom, p.TokenDeposit.MulRaw(int64(req.Quantity))),
		RemainingAllowance: -1,
		Available:          true,
	}
	if c.MintPolicy.PerAddressLimit > 0 {
		used, _ := q.k.PublicMintCount.Get(ctx, collections.Join(c.Id, req.Buyer))
		remaining := int64(c.MintPolicy.PerAddressLimit) - int64(used)
		if remaining < 0 {
			remaining = 0
		}
		resp.RemainingAllowance = remaining
	}
	if uint32(req.Quantity) > p.MaxBatchSize {
		resp.Available, resp.Reason = false, "quantity exceeds max_batch_size"
	} else if err := q.k.publicMintCheck(ctx, c, req.Buyer, req.Quantity, p); err != nil {
		resp.Available, resp.Reason = false, err.Error()
	}
	return resp, nil
}
