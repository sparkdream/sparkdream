package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/artifact/types"
)

// CancelAuction withdraws an auction nobody has bid on yet. Once a bid is
// in, the auction runs to its end.
func (k msgServer) CancelAuction(ctx context.Context, msg *types.MsgCancelAuction) (*types.MsgCancelAuctionResponse, error) {
	if _, err := k.decodeAddr("seller", msg.Seller); err != nil {
		return nil, err
	}
	a, err := k.getAuction(ctx, msg.AuctionId)
	if err != nil {
		return nil, err
	}
	if !k.canActAs(ctx, msg.Seller, a.Seller) {
		return nil, types.ErrNotTokenOwner
	}
	if a.HighBidder != "" {
		return nil, errorsmod.Wrap(types.ErrAuctionActive, "an auction with bids runs to its end")
	}
	if err := k.abortAuction(ctx, a, "cancelled"); err != nil {
		return nil, err
	}
	return &types.MsgCancelAuctionResponse{}, nil
}
