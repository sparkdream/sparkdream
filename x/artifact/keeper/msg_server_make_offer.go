package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// MakeOffer escrows a SPARK offer for one token or, with token_id 0, for
// any token of a class (docs/x-artifact-spec.md §5.13.1). A bidder holds
// at most max_offers_per_bidder open offers.
func (k msgServer) MakeOffer(ctx context.Context, msg *types.MsgMakeOffer) (*types.MsgMakeOfferResponse, error) {
	bidder, err := k.decodeAddr("bidder", msg.Bidder)
	if err != nil {
		return nil, err
	}
	p := k.GetParams(ctx)
	if !p.MarketEnabled {
		return nil, types.ErrMarketDisabled
	}
	if _, err := k.validateRecipient("bidder", msg.Bidder); err != nil {
		return nil, err
	}
	c, err := k.getClass(ctx, msg.ClassId)
	if err != nil {
		return nil, err
	}
	if err := marketable(c); err != nil {
		return nil, err
	}
	if c.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
		return nil, types.ErrContentHidden
	}
	if msg.TokenId != 0 {
		t, err := k.getToken(ctx, msg.ClassId, msg.TokenId)
		if err != nil {
			return nil, err
		}
		if k.sameMember(ctx, t.Owner, msg.Bidder) {
			return nil, errorsmod.Wrap(types.ErrInvalidRecipient, "you already hold this token")
		}
		if t.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
			return nil, types.ErrContentHidden
		}
	} else if msg.ExpectedMetadataHash != "" || msg.ExpectedContentsHash != "" {
		return nil, errorsmod.Wrap(types.ErrInvalidMetadata, "collection offers take no metadata or contents pin")
	}
	if err := k.validatePrice(ctx, msg.Price, false); err != nil {
		return nil, err
	}
	if msg.Duration <= 0 || msg.Duration > p.MaxListingDuration {
		return nil, errorsmod.Wrapf(types.ErrInvalidMetadata, "duration must be in (0, %d] seconds", p.MaxListingDuration)
	}
	if n, err := k.countOffers(ctx, msg.Bidder, p.MaxOffersPerBidder); err != nil {
		return nil, err
	} else if n >= p.MaxOffersPerBidder {
		return nil, errorsmod.Wrapf(types.ErrTooManyOffers, "at most %d open offers", p.MaxOffersPerBidder)
	}
	if err := k.collect(ctx, bidder, msg.Price.Amount); err != nil {
		return nil, err
	}
	seq, err := k.OfferSeq.Next(ctx)
	if err != nil {
		return nil, err
	}
	o := types.Offer{
		Id:                   seq + 1,
		Bidder:               msg.Bidder,
		ClassId:              msg.ClassId,
		TokenId:              msg.TokenId,
		Price:                msg.Price,
		CreatedAt:            now(ctx),
		ExpiresAt:            now(ctx) + msg.Duration,
		ExpectedMetadataHash: msg.ExpectedMetadataHash,
		ExpectedContentsHash: msg.ExpectedContentsHash,
	}
	if err := k.Offers.Set(ctx, o.Id, o); err != nil {
		return nil, err
	}
	if err := k.OffersByToken.Set(ctx, collections.Join3(o.ClassId, o.TokenId, o.Id)); err != nil {
		return nil, err
	}
	if err := k.OffersByBidder.Set(ctx, collections.Join(o.Bidder, o.Id)); err != nil {
		return nil, err
	}
	if err := k.OfferExpiry.Set(ctx, collections.Join(o.ExpiresAt, o.Id)); err != nil {
		return nil, err
	}
	emit(ctx, types.EventOfferMade, classAttr(o.ClassId), tokenAttr(o.TokenId),
		sdk.NewAttribute("offer_id", u64(o.Id)), sdk.NewAttribute("bidder", o.Bidder),
		sdk.NewAttribute("price", o.Price.String()), sdk.NewAttribute("expires_at", i64(o.ExpiresAt)))
	return &types.MsgMakeOfferResponse{OfferId: o.Id}, nil
}
