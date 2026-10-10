package keeper

import (
	"context"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

func (k msgServer) UpdateListing(ctx context.Context, msg *types.MsgUpdateListing) (*types.MsgUpdateListingResponse, error) {
	if _, err := k.decodeAddr("seller", msg.Seller); err != nil {
		return nil, err
	}
	_, t, err := k.listingPreconditions(ctx, msg.Seller, msg.ClassId, msg.TokenId, msg.Price, msg.Duration)
	if err != nil {
		return nil, err
	}
	key := collections.Join(msg.ClassId, msg.TokenId)
	l, err := k.Listings.Get(ctx, key)
	if err != nil || l.Seller != msg.Seller {
		return nil, types.ErrListingNotFound
	}
	if err := k.ListingExpiry.Remove(ctx, collections.Join3(l.ExpiresAt, l.ClassId, l.TokenId)); err != nil {
		return nil, err
	}
	t.ListingSeq++
	l.Price = msg.Price
	l.ExpiresAt = now(ctx) + msg.Duration
	l.Nonce = t.ListingSeq
	if err := k.saveToken(ctx, t); err != nil {
		return nil, err
	}
	if err := k.Listings.Set(ctx, key, l); err != nil {
		return nil, err
	}
	if err := k.ListingExpiry.Set(ctx, collections.Join3(l.ExpiresAt, l.ClassId, l.TokenId)); err != nil {
		return nil, err
	}
	emit(ctx, types.EventListingUpdated, classAttr(l.ClassId), tokenAttr(l.TokenId),
		sdk.NewAttribute("seller", l.Seller), sdk.NewAttribute("price", l.Price.String()),
		sdk.NewAttribute("expires_at", i64(l.ExpiresAt)), sdk.NewAttribute("nonce", u64(l.Nonce)))
	return &types.MsgUpdateListingResponse{Nonce: l.Nonce}, nil
}
