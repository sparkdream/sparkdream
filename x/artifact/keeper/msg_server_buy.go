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
	p := k.GetParams(ctx)
	if !p.MarketEnabled {
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
	if msg.Buyer == l.Seller {
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

	s := ComputeSettlement(l.Price.Amount, p.SaleFeeBps, c.RoyaltyBps, p.MaxRoyaltyBps)
	// Each share is paid to its recipient, or to the seller if the recipient
	// has become unpayable (§7.7).
	royaltyParts := SplitRoyalty(s.Royalty, c.RoyaltyRecipients)
	royaltyTo := make([]string, len(royaltyParts))
	redirected := false
	for i, share := range c.RoyaltyRecipients {
		royaltyTo[i] = share.Address
		if !royaltyParts[i].IsPositive() {
			continue
		}
		if addr, err := k.addressCodec.StringToBytes(share.Address); err != nil || k.bankKeeper.BlockedAddr(addr) {
			royaltyTo[i] = l.Seller
			redirected = true
		}
	}
	if err := k.fundCommunityPool(ctx, buyer, s.Fee); err != nil {
		return nil, err
	}
	paid := make([]string, 0, len(royaltyParts))
	for i, part := range royaltyParts {
		if !part.IsPositive() {
			continue
		}
		if err := k.send(ctx, buyer, royaltyTo[i], part); err != nil {
			return nil, err
		}
		paid = append(paid, royaltyTo[i]+"="+part.String())
	}
	if err := k.send(ctx, buyer, l.Seller, s.Proceeds); err != nil {
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
