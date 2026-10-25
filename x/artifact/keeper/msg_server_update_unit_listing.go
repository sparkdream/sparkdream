package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// UpdateUnitListing changes a unit listing's quantity, per-unit price and
// duration (docs/x-artifact-spec.md §5.8). The locked units follow the new
// quantity; buyers are protected by MsgBuyUnits.expected_unit_price.
func (k msgServer) UpdateUnitListing(ctx context.Context, msg *types.MsgUpdateUnitListing) (*types.MsgUpdateUnitListingResponse, error) {
	if _, err := k.decodeAddr("seller", msg.Seller); err != nil {
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
	if !k.canActAs(ctx, msg.Seller, l.Seller) {
		return nil, types.ErrNotTokenOwner
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
	if msg.Quantity == 0 {
		return nil, errorsmod.Wrap(types.ErrInvalidBatch, "quantity must be positive")
	}
	if err := k.validatePrice(ctx, msg.UnitPrice, false); err != nil {
		return nil, err
	}
	if msg.Duration <= 0 || msg.Duration > p.MaxListingDuration {
		return nil, errorsmod.Wrapf(types.ErrInvalidMetadata, "duration must be in (0, %d] seconds", p.MaxListingDuration)
	}
	switch {
	case msg.Quantity > l.Quantity:
		if err := k.lockUnits(ctx, &e, l.Seller, msg.Quantity-l.Quantity, false); err != nil {
			return nil, err
		}
	case msg.Quantity < l.Quantity:
		if err := k.lockUnits(ctx, &e, l.Seller, l.Quantity-msg.Quantity, true); err != nil {
			return nil, err
		}
	}
	if err := k.saveEdition(ctx, e); err != nil {
		return nil, err
	}
	if err := k.UnitListingExpiry.Remove(ctx, collections.Join(l.ExpiresAt, l.Id)); err != nil {
		return nil, err
	}
	l.Quantity, l.UnitPrice, l.ExpiresAt = msg.Quantity, msg.UnitPrice, now(ctx)+msg.Duration
	if err := k.UnitListings.Set(ctx, l.Id, l); err != nil {
		return nil, err
	}
	if err := k.UnitListingExpiry.Set(ctx, collections.Join(l.ExpiresAt, l.Id)); err != nil {
		return nil, err
	}
	emit(ctx, types.EventUnitsListingUpdated, classAttr(l.ClassId), editionAttr(l.EditionId),
		sdk.NewAttribute("listing_id", u64(l.Id)), sdk.NewAttribute("quantity", u64(l.Quantity)),
		sdk.NewAttribute("unit_price", l.UnitPrice.String()), sdk.NewAttribute("expires_at", i64(l.ExpiresAt)))
	return &types.MsgUpdateUnitListingResponse{}, nil
}
