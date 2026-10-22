package keeper

import (
	"context"

	"sparkdream/x/artifact/types"
)

func (q queryServer) Auction(ctx context.Context, req *types.QueryAuctionRequest) (*types.QueryAuctionResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	a, err := q.k.Auctions.Get(ctx, req.AuctionId)
	if err != nil {
		return nil, notFound(err, "auction")
	}
	return &types.QueryAuctionResponse{Auction: a, MinNextBid: MinNextBid(a).String()}, nil
}
