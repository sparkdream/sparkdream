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

// AcceptRental takes up a rental offer (docs/x-artifact-spec.md §5.10.2):
// the renter pays the price (settled like a sale: fee, royalty, owner
// proceeds) and the token is RENTED-locked until the rental ends. The
// renter pins the price, the offer nonce and, for a token holding nested
// tokens, the contents hash.
func (k msgServer) AcceptRental(ctx context.Context, msg *types.MsgAcceptRental) (*types.MsgAcceptRentalResponse, error) {
	renter, err := k.decodeAddr("renter", msg.Renter)
	if err != nil {
		return nil, err
	}
	key := collections.Join(msg.ClassId, msg.TokenId)
	o, err := k.RentalOffers.Get(ctx, key)
	if err != nil || o.ExpiresAt <= now(ctx) {
		return nil, types.ErrRentalOfferNotFound
	}
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
	if t.Owner != o.Owner {
		return nil, errorsmod.Wrap(types.ErrRentalOfferNotFound, "the token changed hands")
	}
	if t.Lock != types.TokenLock_TOKEN_LOCK_NONE {
		return nil, errorsmod.Wrapf(types.ErrTokenLocked, "token is %s", t.Lock)
	}
	if isTargetHidden(c, t) {
		return nil, types.ErrContentHidden
	}
	if types.IsExpired(t, now(ctx)) {
		return nil, types.ErrTokenExpired
	}
	if k.sameMember(ctx, msg.Renter, o.Owner) || (o.User != "" && !k.canActAs(ctx, msg.Renter, o.User)) {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "this offer is not open to you")
	}
	if !msg.ExpectedPrice.Equal(o.Price) || msg.ExpectedNonce != o.Nonce {
		return nil, errorsmod.Wrapf(types.ErrListingChanged, "offer is %s at nonce %d", o.Price, o.Nonce)
	}
	if msg.ExpectedMetadataHash != "" && msg.ExpectedMetadataHash != types.MetadataHash(t.Metadata) {
		return nil, types.ErrMetadataChanged
	}
	if err := k.checkContentsPin(ctx, t, msg.ExpectedContentsHash); err != nil {
		return nil, err
	}
	expires := now(ctx) + o.Duration
	if t.ExpiresAt > 0 && expires > t.ExpiresAt {
		return nil, errorsmod.Wrap(types.ErrNotRentable, "the rental would outlast the token; renew it first")
	}
	paid, redirected := []string{}, false
	s := Settlement{}
	if o.Price.Amount.IsPositive() {
		if !k.GetParams(ctx).MarketEnabled {
			return nil, types.ErrMarketDisabled
		}
		if s, paid, redirected, err = k.settleSale(ctx, c, renter, o.Owner, o.Price.Amount); err != nil {
			return nil, err
		}
	}
	if err := k.clearRentalOffer(ctx, t.ClassId, t.Id, "accepted"); err != nil {
		return nil, err
	}
	t.Lock = types.TokenLock_TOKEN_LOCK_RENTED
	if err := k.saveToken(ctx, t); err != nil {
		return nil, err
	}
	r := types.Rental{
		ClassId:     t.ClassId,
		TokenId:     t.Id,
		Owner:       o.Owner,
		User:        msg.Renter,
		StartedAt:   now(ctx),
		ExpiresAt:   expires,
		OwnerMayEnd: o.OwnerMayEnd,
		Price:       o.Price,
	}
	if err := k.Rentals.Set(ctx, key, r); err != nil {
		return nil, err
	}
	if err := k.RentalsByUser.Set(ctx, collections.Join3(r.User, r.ClassId, r.TokenId)); err != nil {
		return nil, err
	}
	if err := k.RentalExpiry.Set(ctx, collections.Join3(r.ExpiresAt, r.ClassId, r.TokenId)); err != nil {
		return nil, err
	}
	emit(ctx, types.EventRented, classAttr(t.ClassId), tokenAttr(t.Id),
		sdk.NewAttribute("owner", r.Owner), sdk.NewAttribute("user", r.User), sdk.NewAttribute("price", r.Price.String()),
		sdk.NewAttribute("expires_at", i64(r.ExpiresAt)), sdk.NewAttribute("fee", feeStr(s)),
		sdk.NewAttribute("royalty_recipients", strings.Join(paid, ",")),
		sdk.NewAttribute("royalty_redirected", strconv.FormatBool(redirected)))
	return &types.MsgAcceptRentalResponse{ExpiresAt: r.ExpiresAt}, nil
}

func feeStr(s Settlement) string {
	if s.Fee.IsNil() {
		return "0"
	}
	return s.Fee.String()
}
