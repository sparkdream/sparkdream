package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// Bid places an escrowed bid on an open auction (docs/x-artifact-spec.md
// §5.13.4). It must reach the reserve, or beat the high bid by the
// auction's increment; the previous high bid is refunded at once. A bid in
// the last auction_extension seconds pushes the end out (anti-sniping),
// never to within auction_extension seconds of the expiry of the token or
// anything nested in it.
func (k msgServer) Bid(ctx context.Context, msg *types.MsgBid) (*types.MsgBidResponse, error) {
	bidder, err := k.decodeAddr("bidder", msg.Bidder)
	if err != nil {
		return nil, err
	}
	p := k.GetParams(ctx)
	if !p.MarketEnabled {
		return nil, types.ErrMarketDisabled
	}
	a, err := k.getAuction(ctx, msg.AuctionId)
	if err != nil {
		return nil, err
	}
	if now(ctx) >= a.EndsAt {
		return nil, types.ErrAuctionEnded
	}
	if k.sameMember(ctx, msg.Bidder, a.Seller) {
		return nil, errorsmod.Wrap(types.ErrInvalidRecipient, "cannot bid on your own auction")
	}
	if _, err := k.validateRecipient("bidder", msg.Bidder); err != nil {
		return nil, err
	}
	c, err := k.getClass(ctx, a.ClassId)
	if err != nil {
		return nil, err
	}
	t, err := k.getToken(ctx, a.ClassId, a.TokenId)
	if err != nil {
		return nil, err
	}
	if err := sellable(c, t, now(ctx)); err != nil {
		return nil, err
	}
	if err := k.treeSellable(ctx, t); err != nil {
		return nil, err
	}
	if msg.ExpectedMetadataHash != "" && msg.ExpectedMetadataHash != types.MetadataHash(t.Metadata) {
		return nil, types.ErrMetadataChanged
	}
	if err := k.checkContentsPin(ctx, t, msg.ExpectedContentsHash); err != nil {
		return nil, err
	}
	if err := k.validatePrice(ctx, msg.Amount, false); err != nil {
		return nil, err
	}
	if min := MinNextBid(a); msg.Amount.Amount.LT(min) {
		return nil, errorsmod.Wrapf(types.ErrBidTooLow, "the lowest acceptable bid is %s", min)
	}
	if err := k.collect(ctx, bidder, msg.Amount.Amount); err != nil {
		return nil, err
	}
	if a.HighBidder != "" {
		if err := k.refund(ctx, a.HighBidder, a.HighBid); err != nil {
			return nil, err
		}
	}
	a.HighBidder, a.HighBid = msg.Bidder, msg.Amount.Amount
	// Settlement re-checks the winning bid's pins (bait-and-switch, §12.2).
	a.HighBidMetadataHash, a.HighBidContentsHash = msg.ExpectedMetadataHash, msg.ExpectedContentsHash
	a.Bids++
	if left := a.EndsAt - now(ctx); left < p.AuctionExtension {
		// The extension stops auction_extension seconds short of the
		// tree's expiry (the StartAuction bound), so the settling block
		// still finds a live token.
		newEnd := now(ctx) + p.AuctionExtension
		expiry, err := k.treeExpiry(ctx, t)
		if err != nil {
			return nil, err
		}
		if expiry > 0 && newEnd > expiry-p.AuctionExtension {
			newEnd = expiry - p.AuctionExtension
		}
		if newEnd > a.EndsAt {
			if err := k.AuctionEnds.Remove(ctx, collections.Join(a.EndsAt, a.Id)); err != nil {
				return nil, err
			}
			a.EndsAt = newEnd
			if err := k.AuctionEnds.Set(ctx, collections.Join(a.EndsAt, a.Id)); err != nil {
				return nil, err
			}
		}
	}
	if err := k.Auctions.Set(ctx, a.Id, a); err != nil {
		return nil, err
	}
	emit(ctx, types.EventBidPlaced, classAttr(a.ClassId), tokenAttr(a.TokenId),
		sdk.NewAttribute("auction_id", u64(a.Id)), sdk.NewAttribute("bidder", msg.Bidder),
		sdk.NewAttribute("amount", msg.Amount.String()), sdk.NewAttribute("ends_at", i64(a.EndsAt)))
	return &types.MsgBidResponse{EndsAt: a.EndsAt}, nil
}
