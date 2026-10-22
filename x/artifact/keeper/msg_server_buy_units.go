package keeper

import (
	"context"
	"strconv"
	"strings"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// BuyUnits buys quantity units of a unit listing, settling price × quantity
// like MsgBuy (fee, royalty, proceeds). The buyer pays the deposit of a new
// balance record.
func (k msgServer) BuyUnits(ctx context.Context, msg *types.MsgBuyUnits) (*types.MsgBuyUnitsResponse, error) {
	buyer, err := k.decodeAddr("buyer", msg.Buyer)
	if err != nil {
		return nil, err
	}
	p := k.GetParams(ctx)
	if !p.MarketEnabled {
		return nil, types.ErrMarketDisabled
	}
	l, err := k.getUnitListing(ctx, msg.ListingId)
	if err != nil {
		return nil, err
	}
	if l.ExpiresAt <= now(ctx) {
		return nil, types.ErrListingExpired
	}
	c, err := k.getEditionsClass(ctx, l.ClassId)
	if err != nil {
		return nil, err
	}
	e, err := k.getEdition(ctx, l.ClassId, l.EditionId)
	if err != nil {
		return nil, err
	}
	if isEditionHidden(c, e) {
		return nil, types.ErrContentHidden
	}
	if msg.Buyer == l.Seller {
		return nil, errorsmod.Wrap(types.ErrInvalidRecipient, "cannot buy your own listing")
	}
	if _, err := k.validateRecipient("buyer", msg.Buyer); err != nil {
		return nil, err
	}
	if msg.Quantity == 0 || msg.Quantity > l.Quantity {
		return nil, errorsmod.Wrapf(types.ErrInsufficientUnits, "listing has %d units", l.Quantity)
	}
	if !msg.ExpectedUnitPrice.Equal(l.UnitPrice) {
		return nil, errorsmod.Wrapf(types.ErrListingChanged, "listing is %s per unit", l.UnitPrice)
	}
	if msg.ExpectedMetadataHash != "" && msg.ExpectedMetadataHash != types.MetadataHash(e.Metadata) {
		return nil, types.ErrMetadataChanged
	}

	price := l.UnitPrice.Amount.Mul(math.NewIntFromUint64(msg.Quantity))
	s, paid, redirected, err := k.settleSale(ctx, c, buyer, l.Seller, price)
	if err != nil {
		return nil, err
	}
	if err := k.debit(ctx, &e, l.Seller, msg.Quantity, true); err != nil {
		return nil, err
	}
	if err := k.credit(ctx, &e, msg.Buyer, msg.Quantity, buyer, false, math.Int{}, ""); err != nil {
		return nil, err
	}
	if err := k.saveEdition(ctx, e); err != nil {
		return nil, err
	}
	l.Quantity -= msg.Quantity
	if l.Quantity == 0 {
		if err := k.removeUnitListing(ctx, l, "sold"); err != nil {
			return nil, err
		}
	} else if err := k.UnitListings.Set(ctx, l.Id, l); err != nil {
		return nil, err
	}
	emit(ctx, types.EventUnitsSold, classAttr(c.Id), editionAttr(e.Id),
		sdk.NewAttribute("listing_id", u64(l.Id)), sdk.NewAttribute("seller", l.Seller), sdk.NewAttribute("buyer", msg.Buyer),
		sdk.NewAttribute("quantity", u64(msg.Quantity)), sdk.NewAttribute("price", sdk.NewCoin(k.BondDenom(ctx), price).String()),
		sdk.NewAttribute("fee", s.Fee.String()), sdk.NewAttribute("royalty", s.Royalty.String()),
		sdk.NewAttribute("royalty_recipients", strings.Join(paid, ",")),
		sdk.NewAttribute("royalty_redirected", strconv.FormatBool(redirected)))
	return &types.MsgBuyUnitsResponse{Fee: s.Fee.String(), Royalty: s.Royalty.String(), Proceeds: s.Proceeds.String()}, nil
}
