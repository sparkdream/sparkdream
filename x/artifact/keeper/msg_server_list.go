package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// listingPreconditions checks what MsgList and MsgUpdateListing share.
func (k Keeper) listingPreconditions(ctx context.Context, seller string, classID, tokenID uint64, price sdk.Coin, duration int64) (types.Class, types.Token, error) {
	p := k.GetParams(ctx)
	if !p.MarketEnabled {
		return types.Class{}, types.Token{}, types.ErrMarketDisabled
	}
	c, err := k.getClass(ctx, classID)
	if err != nil {
		return c, types.Token{}, err
	}
	t, err := k.getToken(ctx, classID, tokenID)
	if err != nil {
		return c, t, err
	}
	if !c.Flags.Transferable {
		return c, t, types.ErrNotTransferable
	}
	if isTargetHidden(c, t) {
		return c, t, types.ErrContentHidden
	}
	if t.Owner != seller {
		return c, t, types.ErrNotTokenOwner
	}
	if err := k.validatePrice(ctx, price, false); err != nil {
		return c, t, err
	}
	if duration <= 0 || duration > p.MaxListingDuration {
		return c, t, errorsmod.Wrapf(types.ErrInvalidMetadata, "duration must be in (0, %d] seconds", p.MaxListingDuration)
	}
	return c, t, nil
}

func (k msgServer) List(ctx context.Context, msg *types.MsgList) (*types.MsgListResponse, error) {
	if _, err := k.decodeAddr("seller", msg.Seller); err != nil {
		return nil, err
	}
	_, t, err := k.listingPreconditions(ctx, msg.Seller, msg.ClassId, msg.TokenId, msg.Price, msg.Duration)
	if err != nil {
		return nil, err
	}
	if t.Lock != types.TokenLock_TOKEN_LOCK_NONE {
		return nil, errorsmod.Wrapf(types.ErrTokenLocked, "token is %s", t.Lock)
	}
	t.ListingSeq++
	t.Lock = types.TokenLock_TOKEN_LOCK_LISTED
	l := types.Listing{
		ClassId:   t.ClassId,
		TokenId:   t.Id,
		Seller:    msg.Seller,
		Price:     msg.Price,
		CreatedAt: now(ctx),
		ExpiresAt: now(ctx) + msg.Duration,
		Nonce:     t.ListingSeq,
	}
	if err := k.saveToken(ctx, t); err != nil {
		return nil, err
	}
	if err := k.Listings.Set(ctx, collections.Join(t.ClassId, t.Id), l); err != nil {
		return nil, err
	}
	if err := k.ListingsBySeller.Set(ctx, collections.Join3(l.Seller, t.ClassId, t.Id)); err != nil {
		return nil, err
	}
	if err := k.ListingExpiry.Set(ctx, collections.Join3(l.ExpiresAt, t.ClassId, t.Id)); err != nil {
		return nil, err
	}
	emit(ctx, types.EventListed, classAttr(t.ClassId), tokenAttr(t.Id),
		sdk.NewAttribute("seller", l.Seller), sdk.NewAttribute("price", l.Price.String()),
		sdk.NewAttribute("expires_at", i64(l.ExpiresAt)), sdk.NewAttribute("nonce", u64(l.Nonce)))
	return &types.MsgListResponse{Nonce: l.Nonce}, nil
}
