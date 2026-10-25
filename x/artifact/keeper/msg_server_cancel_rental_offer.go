package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/artifact/types"
)

// CancelRentalOffer withdraws an open rental offer (the offering owner or
// the token's current owner).
func (k msgServer) CancelRentalOffer(ctx context.Context, msg *types.MsgCancelRentalOffer) (*types.MsgCancelRentalOfferResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	o, err := k.RentalOffers.Get(ctx, collections.Join(msg.ClassId, msg.TokenId))
	if err != nil {
		return nil, types.ErrRentalOfferNotFound
	}
	if !k.canActAs(ctx, msg.Owner, o.Owner) {
		t, err := k.getToken(ctx, msg.ClassId, msg.TokenId)
		if err != nil || !k.canActAs(ctx, msg.Owner, t.Owner) {
			return nil, types.ErrNotTokenOwner
		}
	}
	if err := k.clearRentalOffer(ctx, msg.ClassId, msg.TokenId, "owner"); err != nil {
		return nil, err
	}
	return &types.MsgCancelRentalOfferResponse{}, nil
}
