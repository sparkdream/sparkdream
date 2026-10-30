package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

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
	// A sale that cannot settle ends the auction without one (the high bid
	// is refunded and the token unlocked), so a bid is never stranded.
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	cacheCtx, write := sdkCtx.CacheContext()
	sold, err := k.settleAuction(cacheCtx, a)
	if err == nil {
		write()
		return &types.MsgSettleAuctionResponse{Sold: sold}, nil
	}
	sdkCtx.Logger().Error("artifact: auction settlement failed; aborting", "auction_id", a.Id, "error", err)
	if err := k.abortAuction(sdkCtx, a, "settlement_failed"); err != nil {
		return nil, err
	}
	return &types.MsgSettleAuctionResponse{Sold: false}, nil
}
