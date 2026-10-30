package keeper

import (
	"context"
	"strconv"
	"strings"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

func (k msgServer) Buy(ctx context.Context, msg *types.MsgBuy) (*types.MsgBuyResponse, error) {
	buyer, err := k.decodeAddr("buyer", msg.Buyer)
	if err != nil {
		return nil, err
	}
	if !k.GetParams(ctx).MarketEnabled {
		return nil, types.ErrMarketDisabled
	}
	l, err := k.Listings.Get(ctx, collections.Join(msg.ClassId, msg.TokenId))
	if err != nil {
		return nil, types.ErrListingNotFound
	}
	if l.ExpiresAt <= now(ctx) {
		return nil, types.ErrListingExpired
	}
	c, err := k.getClass(ctx, msg.ClassId)
	if err != nil {
		return nil, err
	}
	t, err := k.getToken(ctx, msg.ClassId, msg.TokenId)
	if err != nil {
		return nil, err
	}
	if isTargetHidden(c, t) {
		return nil, types.ErrContentHidden
	}
	if types.IsExpired(t, now(ctx)) {
		return nil, types.ErrTokenExpired
	}
	if t.RedeemedAt != 0 && !c.Flags.RedeemedTransferable {
		return nil, types.ErrRedeemedNotTransferable
	}
	if err := k.treeSellable(ctx, t); err != nil {
		return nil, err
	}
	if k.sameMember(ctx, msg.Buyer, l.Seller) {
		return nil, errorsmod.Wrap(types.ErrInvalidRecipient, "cannot buy your own listing")
	}
	if _, err := k.validateRecipient("buyer", msg.Buyer); err != nil {
		return nil, err
	}
	if !msg.ExpectedPrice.Equal(l.Price) || msg.ExpectedNonce != l.Nonce {
		return nil, errorsmod.Wrapf(types.ErrListingChanged, "listing is %s at nonce %d", l.Price, l.Nonce)
	}
	if msg.ExpectedMetadataHash != "" && msg.ExpectedMetadataHash != types.MetadataHash(t.Metadata) {
		return nil, types.ErrMetadataChanged
	}

	// A token holding nested tokens needs the contents pin (fail closed).
	if err := k.checkContentsPin(ctx, t, msg.ExpectedContentsHash); err != nil {
		return nil, err
	}

	s, paid, redirected, err := k.settleSale(ctx, c, buyer, l.Seller, l.Price.Amount)
	if err != nil {
		return nil, err
	}

	if err := k.removeListing(ctx, l.ClassId, l.TokenId, "sold"); err != nil {
		return nil, err
	}
	t, err = k.getToken(ctx, l.ClassId, l.TokenId)
	if err != nil {
		return nil, err
	}
	if err := k.moveToken(ctx, &t, msg.Buyer); err != nil {
		return nil, err
	}
	emit(ctx, types.EventTransferred, classAttr(t.ClassId), tokenAttr(t.Id),
		sdk.NewAttribute("from", l.Seller), sdk.NewAttribute("to", msg.Buyer), sdk.NewAttribute("via", "sale"))
	emit(ctx, types.EventSold, classAttr(t.ClassId), tokenAttr(t.Id),
		sdk.NewAttribute("seller", l.Seller), sdk.NewAttribute("buyer", msg.Buyer),
		sdk.NewAttribute("price", l.Price.String()), sdk.NewAttribute("fee", s.Fee.String()),
		sdk.NewAttribute("royalty", s.Royalty.String()), sdk.NewAttribute("royalty_recipients", strings.Join(paid, ",")),
		sdk.NewAttribute("royalty_redirected", strconv.FormatBool(redirected)))
	return &types.MsgBuyResponse{Fee: s.Fee.String(), Royalty: s.Royalty.String(), Proceeds: s.Proceeds.String()}, nil
}
