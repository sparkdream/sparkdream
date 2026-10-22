package keeper

import (
	"context"
	"strings"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// AcceptOffer sells a token the signer holds to an offer's bidder,
// settling the escrowed price like a sale (docs/x-artifact-spec.md
// §5.13.2). A listing is cancelled implicitly; any other lock refuses. A
// collection offer never buys a token holding nested tokens (the bidder
// could not pin its contents).
func (k msgServer) AcceptOffer(ctx context.Context, msg *types.MsgAcceptOffer) (*types.MsgAcceptOfferResponse, error) {
	if _, err := k.decodeAddr("seller", msg.Seller); err != nil {
		return nil, err
	}
	if !k.GetParams(ctx).MarketEnabled {
		return nil, types.ErrMarketDisabled
	}
	o, err := k.getOffer(ctx, msg.OfferId)
	if err != nil {
		return nil, err
	}
	if o.ExpiresAt <= now(ctx) {
		return nil, errorsmod.Wrap(types.ErrOfferNotFound, "offer expired")
	}
	if !msg.ExpectedPrice.Equal(o.Price) {
		return nil, errorsmod.Wrapf(types.ErrListingChanged, "offer is %s", o.Price)
	}
	tokenID := o.TokenId
	if tokenID == 0 {
		tokenID = msg.TokenId
	} else if msg.TokenId != 0 && msg.TokenId != o.TokenId {
		return nil, errorsmod.Wrapf(types.ErrInvalidBatch, "offer %d is for token %d", o.Id, o.TokenId)
	}
	c, err := k.getClass(ctx, o.ClassId)
	if err != nil {
		return nil, err
	}
	if err := marketable(c); err != nil {
		return nil, err
	}
	t, err := k.getToken(ctx, o.ClassId, tokenID)
	if err != nil {
		return nil, err
	}
	if t.Owner != msg.Seller {
		return nil, types.ErrNotTokenOwner
	}
	if t.Owner == o.Bidder {
		return nil, errorsmod.Wrap(types.ErrInvalidRecipient, "the bidder already holds this token")
	}
	if err := sellable(c, t, now(ctx)); err != nil {
		return nil, err
	}
	if o.ExpectedMetadataHash != "" && o.ExpectedMetadataHash != types.MetadataHash(t.Metadata) {
		return nil, types.ErrMetadataChanged
	}
	if err := k.checkContentsPin(ctx, t, o.ExpectedContentsHash); err != nil {
		return nil, err
	}
	switch t.Lock {
	case types.TokenLock_TOKEN_LOCK_NONE:
	case types.TokenLock_TOKEN_LOCK_LISTED:
		if err := k.removeListing(ctx, t.ClassId, t.Id, "sold"); err != nil {
			return nil, err
		}
		if t, err = k.getToken(ctx, o.ClassId, tokenID); err != nil {
			return nil, err
		}
	default:
		return nil, errorsmod.Wrapf(types.ErrTokenLocked, "token is %s", t.Lock)
	}
	if err := k.removeOffer(ctx, o, false, "accepted"); err != nil {
		return nil, err
	}
	s, paid, redirected, err := k.sellFromEscrow(ctx, c, &t, o.Bidder, o.Price.Amount)
	if err != nil {
		return nil, err
	}
	emit(ctx, types.EventSold, classAttr(t.ClassId), tokenAttr(t.Id),
		sdk.NewAttribute("seller", msg.Seller), sdk.NewAttribute("buyer", o.Bidder),
		sdk.NewAttribute("price", o.Price.String()), sdk.NewAttribute("fee", s.Fee.String()),
		sdk.NewAttribute("royalty", s.Royalty.String()), sdk.NewAttribute("royalty_recipients", strings.Join(paid, ",")),
		sdk.NewAttribute("royalty_redirected", boolStr(redirected)), sdk.NewAttribute("offer_id", u64(o.Id)))
	return &types.MsgAcceptOfferResponse{Fee: s.Fee.String(), Royalty: s.Royalty.String(), Proceeds: s.Proceeds.String()}, nil
}
