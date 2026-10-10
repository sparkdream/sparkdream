package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/artifact/types"
)

// Delist is always allowed (market disabled, token hidden) so a seller can
// always get their token back.
func (k msgServer) Delist(ctx context.Context, msg *types.MsgDelist) (*types.MsgDelistResponse, error) {
	if _, err := k.decodeAddr("seller", msg.Seller); err != nil {
		return nil, err
	}
	l, err := k.Listings.Get(ctx, collections.Join(msg.ClassId, msg.TokenId))
	if err != nil || l.Seller != msg.Seller {
		return nil, types.ErrListingNotFound
	}
	if err := k.removeListing(ctx, msg.ClassId, msg.TokenId, "seller"); err != nil {
		return nil, err
	}
	return &types.MsgDelistResponse{}, nil
}
