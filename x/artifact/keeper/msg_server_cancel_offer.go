package keeper

import (
	"context"

	"sparkdream/x/artifact/types"
)

// CancelOffer withdraws an offer and refunds its escrow. Always allowed,
// even with the market disabled, so a bidder can always get their SPARK back.
func (k msgServer) CancelOffer(ctx context.Context, msg *types.MsgCancelOffer) (*types.MsgCancelOfferResponse, error) {
	if _, err := k.decodeAddr("bidder", msg.Bidder); err != nil {
		return nil, err
	}
	o, err := k.getOffer(ctx, msg.OfferId)
	if err != nil {
		return nil, err
	}
	if !k.canActAs(ctx, msg.Bidder, o.Bidder) {
		return nil, types.ErrNotAuthorized
	}
	if err := k.removeOffer(ctx, o, true, "cancelled"); err != nil {
		return nil, err
	}
	return &types.MsgCancelOfferResponse{}, nil
}
