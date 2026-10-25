package keeper

import (
	"context"

	"sparkdream/x/artifact/types"
)

// DelistUnits cancels a unit listing. Always allowed, even with the market
// disabled or the edition hidden, so a seller can always unlock their units.
func (k msgServer) DelistUnits(ctx context.Context, msg *types.MsgDelistUnits) (*types.MsgDelistUnitsResponse, error) {
	if _, err := k.decodeAddr("seller", msg.Seller); err != nil {
		return nil, err
	}
	l, err := k.getUnitListing(ctx, msg.ListingId)
	if err != nil {
		return nil, err
	}
	if !k.canActAs(ctx, msg.Seller, l.Seller) {
		return nil, types.ErrNotTokenOwner
	}
	if err := k.removeUnitListing(ctx, l, "seller"); err != nil {
		return nil, err
	}
	return &types.MsgDelistUnitsResponse{}, nil
}
