package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/artifact/types"
)

// EndRental ends a rental: the renter may hand it back at any time, the
// owner only when the offer allowed early ends or once it has run out
// (the EndBlocker also ends expired rentals).
func (k msgServer) EndRental(ctx context.Context, msg *types.MsgEndRental) (*types.MsgEndRentalResponse, error) {
	if _, err := k.decodeAddr("signer", msg.Signer); err != nil {
		return nil, err
	}
	r, err := k.Rentals.Get(ctx, collections.Join(msg.ClassId, msg.TokenId))
	if err != nil {
		return nil, types.ErrRentalNotFound
	}
	reason := ""
	switch {
	case k.canActAs(ctx, msg.Signer, r.User):
		reason = "user"
	case k.canActAs(ctx, msg.Signer, r.Owner) && (r.OwnerMayEnd || r.ExpiresAt <= now(ctx)):
		reason = "owner"
	default:
		return nil, types.ErrRentalActive
	}
	if err := k.endRental(ctx, r, reason); err != nil {
		return nil, err
	}
	return &types.MsgEndRentalResponse{}, nil
}
