package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// OfferRental offers a token's user right for duration seconds at a fixed
// SPARK price (docs/x-artifact-spec.md §5.10.1). Nothing is locked: the
// offer is checked against the token when accepted, and a new offer
// replaces the old one. The nonce comes from Token.listing_seq, so a
// signed acceptance never matches a later offer.
func (k msgServer) OfferRental(ctx context.Context, msg *types.MsgOfferRental) (*types.MsgOfferRentalResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	p := k.GetParams(ctx)
	c, err := k.getClass(ctx, msg.ClassId)
	if err != nil {
		return nil, err
	}
	if err := rentable(c); err != nil {
		return nil, err
	}
	t, err := k.getToken(ctx, msg.ClassId, msg.TokenId)
	if err != nil {
		return nil, err
	}
	if !k.canActAs(ctx, msg.Owner, t.Owner) {
		return nil, types.ErrNotTokenOwner
	}
	if t.Lock == types.TokenLock_TOKEN_LOCK_NESTED {
		return nil, errorsmod.Wrap(types.ErrTokenLocked, "nested tokens rent out with their parent")
	}
	if isTargetHidden(c, t) {
		return nil, types.ErrContentHidden
	}
	if msg.User != "" {
		if k.sameMember(ctx, msg.User, msg.Owner) {
			return nil, errorsmod.Wrap(types.ErrInvalidRecipient, "cannot rent to yourself")
		}
		if _, err := k.validateRecipient("user", msg.User); err != nil {
			return nil, err
		}
	}
	if err := k.validatePrice(ctx, msg.Price, true); err != nil {
		return nil, err
	}
	if msg.Duration <= 0 || msg.Duration > p.MaxRentalDuration {
		return nil, errorsmod.Wrapf(types.ErrNotRentable, "duration must be in (0, %d] seconds", p.MaxRentalDuration)
	}
	if msg.OfferDuration <= 0 || msg.OfferDuration > p.MaxListingDuration {
		return nil, errorsmod.Wrapf(types.ErrNotRentable, "offer_duration must be in (0, %d] seconds", p.MaxListingDuration)
	}
	if err := k.clearRentalOffer(ctx, t.ClassId, t.Id, "replaced"); err != nil {
		return nil, err
	}
	t.ListingSeq++
	if err := k.saveToken(ctx, t); err != nil {
		return nil, err
	}
	o := types.RentalOffer{
		ClassId:     t.ClassId,
		TokenId:     t.Id,
		Owner:       t.Owner, // the stored owner, which a migrated signer acts for (ART-102)
		User:        msg.User,
		Price:       msg.Price,
		Duration:    msg.Duration,
		OwnerMayEnd: msg.OwnerMayEnd,
		CreatedAt:   now(ctx),
		ExpiresAt:   now(ctx) + msg.OfferDuration,
		Nonce:       t.ListingSeq,
	}
	key := collections.Join(t.ClassId, t.Id)
	if err := k.RentalOffers.Set(ctx, key, o); err != nil {
		return nil, err
	}
	if err := k.RentalOfferExpiry.Set(ctx, collections.Join3(o.ExpiresAt, t.ClassId, t.Id)); err != nil {
		return nil, err
	}
	emit(ctx, types.EventRentalOffered, classAttr(t.ClassId), tokenAttr(t.Id),
		sdk.NewAttribute("owner", o.Owner), sdk.NewAttribute("user", o.User), sdk.NewAttribute("price", o.Price.String()),
		sdk.NewAttribute("duration", i64(o.Duration)), sdk.NewAttribute("owner_may_end", boolStr(o.OwnerMayEnd)),
		sdk.NewAttribute("expires_at", i64(o.ExpiresAt)), sdk.NewAttribute("nonce", u64(o.Nonce)))
	return &types.MsgOfferRentalResponse{Nonce: o.Nonce}, nil
}
