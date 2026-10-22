package keeper

import (
	"context"

	"sparkdream/x/artifact/types"
)

// SettleAuction settles an ended auction; anyone may sign, and the
// EndBlocker does it too.
func (k msgServer) SettleAuction(ctx context.Context, msg *types.MsgSettleAuction) (*types.MsgSettleAuctionResponse, error) {
	if _, err := k.decodeAddr("signer", msg.Signer); err != nil {
		return nil, err
	}
	a, err := k.getAuction(ctx, msg.AuctionId)
	if err != nil {
		return nil, err
	}
	if now(ctx) < a.EndsAt {
		return nil, types.ErrAuctionActive
	}
	sold, err := k.settleAuction(ctx, a)
	if err != nil {
		return nil, err
	}
	return &types.MsgSettleAuctionResponse{Sold: sold}, nil
}
