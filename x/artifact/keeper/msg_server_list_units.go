package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// ListUnits offers quantity units at a fixed per-unit price. The units are
// locked in the seller's balance until sold, delisted or expired; nothing
// else is escrowed.
func (k msgServer) ListUnits(ctx context.Context, msg *types.MsgListUnits) (*types.MsgListUnitsResponse, error) {
	if _, err := k.decodeAddr("seller", msg.Seller); err != nil {
		return nil, err
	}
	p := k.GetParams(ctx)
	if !p.MarketEnabled {
		return nil, types.ErrMarketDisabled
	}
	c, err := k.getEditionsClass(ctx, msg.ClassId)
	if err != nil {
		return nil, err
	}
	if !c.Flags.Transferable {
		return nil, types.ErrNotTransferable
	}
	e, err := k.getEdition(ctx, c.Id, msg.EditionId)
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
	// The listing names the address that holds the units (a predecessor of
	// the seller after a key migration) so later unlock/debit find them.
	seller := k.balanceHolder(ctx, c.Id, e.Id, msg.Seller)
	if err := k.lockUnits(ctx, &e, seller, msg.Quantity, false); err != nil {
		return nil, err
	}
	seq, err := k.UnitListingSeq.Next(ctx)
	if err != nil {
		return nil, err
	}
	l := types.UnitListing{
		Id:        seq + 1,
		ClassId:   c.Id,
		EditionId: e.Id,
		Seller:    seller,
		Quantity:  msg.Quantity,
		UnitPrice: msg.UnitPrice,
		CreatedAt: now(ctx),
		ExpiresAt: now(ctx) + msg.Duration,
	}
	if err := k.UnitListings.Set(ctx, l.Id, l); err != nil {
		return nil, err
	}
	if err := k.UnitListingsByEdition.Set(ctx, collections.Join3(l.ClassId, l.EditionId, l.Id)); err != nil {
		return nil, err
	}
	if err := k.UnitListingsBySeller.Set(ctx, collections.Join(l.Seller, l.Id)); err != nil {
		return nil, err
	}
	if err := k.UnitListingExpiry.Set(ctx, collections.Join(l.ExpiresAt, l.Id)); err != nil {
		return nil, err
	}
	if err := k.saveEdition(ctx, e); err != nil {
		return nil, err
	}
	emit(ctx, types.EventUnitsListed, classAttr(c.Id), editionAttr(e.Id),
		sdk.NewAttribute("listing_id", u64(l.Id)), sdk.NewAttribute("seller", l.Seller),
		sdk.NewAttribute("quantity", u64(l.Quantity)), sdk.NewAttribute("unit_price", l.UnitPrice.String()),
		sdk.NewAttribute("expires_at", i64(l.ExpiresAt)))
	return &types.MsgListUnitsResponse{ListingId: l.Id}, nil
}
