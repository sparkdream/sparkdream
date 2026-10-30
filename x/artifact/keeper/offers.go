package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// Escrowed offers and English auctions (docs/x-artifact-spec.md §5.13,
// §7.27). Bids are SPARK held in the module account until they settle or
// come back; settlement runs through settleSale with the module as payer.

// marketable checks what every offer, auction and sale of a unique token
// needs from its class: a transferable UNIQUE class that is not a
// Harberger class (those are always for sale at their own price).
func marketable(c types.Class) error {
	switch {
	case c.IsEditions():
		return errorsmod.Wrap(types.ErrWrongTokenModel, "edition units trade through unit listings")
	case !c.Flags.Transferable:
		return types.ErrNotTransferable
	case c.IsHarberger():
		return types.ErrHarbergerToken
	}
	return nil
}

// sellable checks that a token may be sold now (shared by offers and
// auctions): visible, unexpired, not a spent non-transferable ticket.
func sellable(c types.Class, t types.Token, now int64) error {
	if isTargetHidden(c, t) {
		return types.ErrContentHidden
	}
	if types.IsExpired(t, now) {
		return types.ErrTokenExpired
	}
	if t.RedeemedAt != 0 && !c.Flags.RedeemedTransferable {
		return types.ErrRedeemedNotTransferable
	}
	return nil
}

// treeSellable applies sellable to everything nested under t: a tree is
// sold whole, so a hidden or expired descendant, or a spent
// non-transferable ticket inside it, must not ride along with a sellable
// root (bounded by max_nested_tokens).
func (k Keeper) treeSellable(ctx context.Context, t types.Token) error {
	if t.Descendants == 0 {
		return nil
	}
	desc, err := k.descendantsOf(ctx, t)
	if err != nil {
		return err
	}
	for _, d := range desc {
		dc, err := k.getClass(ctx, d.ClassId)
		if err != nil {
			return err
		}
		if err := sellable(dc, d, now(ctx)); err != nil {
			return errorsmod.Wrapf(err, "nested token %d/%d", d.ClassId, d.Id)
		}
	}
	return nil
}

// treeExpiry is the earliest expires_at of t and everything nested under
// it (0 when nothing in the tree expires). An auction must end at least
// auction_extension seconds before it.
func (k Keeper) treeExpiry(ctx context.Context, t types.Token) (int64, error) {
	earliest := t.ExpiresAt
	if t.Descendants == 0 {
		return earliest, nil
	}
	desc, err := k.descendantsOf(ctx, t)
	if err != nil {
		return 0, err
	}
	for _, d := range desc {
		if d.ExpiresAt > 0 && (earliest == 0 || d.ExpiresAt < earliest) {
			earliest = d.ExpiresAt
		}
	}
	return earliest, nil
}

func (k Keeper) getOffer(ctx context.Context, id uint64) (types.Offer, error) {
	o, err := k.Offers.Get(ctx, id)
	if errors.Is(err, collections.ErrNotFound) {
		return o, errorsmod.Wrapf(types.ErrOfferNotFound, "offer %d", id)
	}
	return o, err
}

// removeOffer deletes an offer and its indexes; refund returns the escrow
// to the bidder (cancel, expiry), otherwise the caller spent it.
func (k Keeper) removeOffer(ctx context.Context, o types.Offer, refund bool, reason string) error {
	if err := k.Offers.Remove(ctx, o.Id); err != nil {
		return err
	}
	if err := k.OffersByToken.Remove(ctx, collections.Join3(o.ClassId, o.TokenId, o.Id)); err != nil {
		return err
	}
	if err := k.OffersByBidder.Remove(ctx, collections.Join(o.Bidder, o.Id)); err != nil {
		return err
	}
	if err := k.OfferExpiry.Remove(ctx, collections.Join(o.ExpiresAt, o.Id)); err != nil {
		return err
	}
	if refund {
		if err := k.refund(ctx, o.Bidder, o.Price.Amount); err != nil {
			return err
		}
	}
	emit(ctx, types.EventOfferClosed, classAttr(o.ClassId), tokenAttr(o.TokenId),
		sdk.NewAttribute("offer_id", u64(o.Id)), sdk.NewAttribute("bidder", o.Bidder),
		sdk.NewAttribute("reason", reason), sdk.NewAttribute("refund", boolStr(refund)))
	return nil
}

// countOffers counts a bidder's open offers, stopping at limit.
func (k Keeper) countOffers(ctx context.Context, bidder string, limit uint32) (uint32, error) {
	var n uint32
	err := k.OffersByBidder.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](bidder), func(collections.Pair[string, uint64]) (bool, error) {
		n++
		return n >= limit, nil
	})
	return n, err
}

// sellFromEscrow settles a sale whose price already sits in the module
// account (accepted offer, settled auction) and moves the token to the
// buyer, who asked for it, so no receive policy applies.
func (k Keeper) sellFromEscrow(ctx context.Context, c types.Class, t *types.Token, buyer string, price math.Int) (Settlement, []string, bool, error) {
	seller := t.Owner
	s, paid, redirected, err := k.settleSale(ctx, c, k.ModuleAddress(), seller, price)
	if err != nil {
		return s, nil, false, err
	}
	if err := k.moveTokenAt(ctx, t, buyer, price); err != nil {
		return s, nil, false, err
	}
	emit(ctx, types.EventTransferred, classAttr(t.ClassId), tokenAttr(t.Id),
		sdk.NewAttribute("from", seller), sdk.NewAttribute("to", buyer), sdk.NewAttribute("via", "sale"))
	return s, paid, redirected, nil
}

// ---------------------------------------------------------------------------
// Auctions
// ---------------------------------------------------------------------------

func (k Keeper) getAuction(ctx context.Context, id uint64) (types.Auction, error) {
	a, err := k.Auctions.Get(ctx, id)
	if errors.Is(err, collections.ErrNotFound) {
		return a, errorsmod.Wrapf(types.ErrAuctionNotFound, "auction %d", id)
	}
	return a, err
}

// MinNextBid is the lowest bid an auction accepts now: the reserve, or the
// high bid raised by min_increment_bps (rounded up, and always above it).
func MinNextBid(a types.Auction) math.Int {
	if a.HighBidder == "" {
		return a.ReservePrice.Amount
	}
	num := a.HighBid.MulRaw(int64(10000 + a.MinIncrementBps))
	next := num.QuoRaw(10000)
	if !num.ModRaw(10000).IsZero() {
		next = next.AddRaw(1)
	}
	if !next.GT(a.HighBid) {
		next = a.HighBid.AddRaw(1)
	}
	return next
}

// closeAuction removes an auction and its indexes and unlocks its token.
// The caller has already settled or refunded the high bid.
func (k Keeper) closeAuction(ctx context.Context, a types.Auction) error {
	if err := k.Auctions.Remove(ctx, a.Id); err != nil {
		return err
	}
	if err := k.AuctionByToken.Remove(ctx, collections.Join(a.ClassId, a.TokenId)); err != nil {
		return err
	}
	if err := k.AuctionEnds.Remove(ctx, collections.Join(a.EndsAt, a.Id)); err != nil {
		return err
	}
	if err := k.AuctionsBySeller.Remove(ctx, collections.Join(a.Seller, a.Id)); err != nil {
		return err
	}
	if t, err := k.getToken(ctx, a.ClassId, a.TokenId); err == nil && t.Lock == types.TokenLock_TOKEN_LOCK_AUCTION {
		t.Lock = types.TokenLock_TOKEN_LOCK_NONE
		return k.saveToken(ctx, t)
	}
	return nil
}

// abortAuction ends an auction without a sale (hide, burn, cancel): the
// high bid goes back to its bidder and the token is unlocked.
func (k Keeper) abortAuction(ctx context.Context, a types.Auction, reason string) error {
	if a.HighBidder != "" {
		if err := k.refund(ctx, a.HighBidder, a.HighBid); err != nil {
			return err
		}
	}
	if err := k.closeAuction(ctx, a); err != nil {
		return err
	}
	emit(ctx, types.EventAuctionSettled, classAttr(a.ClassId), tokenAttr(a.TokenId),
		sdk.NewAttribute("auction_id", u64(a.Id)), sdk.NewAttribute("sold", "false"), sdk.NewAttribute("reason", reason))
	return nil
}

// abortAuctionOf ends a token's auction, if any, without a sale.
func (k Keeper) abortAuctionOf(ctx context.Context, classID, tokenID uint64, reason string) error {
	id, err := k.AuctionByToken.Get(ctx, collections.Join(classID, tokenID))
	if errors.Is(err, collections.ErrNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	a, err := k.getAuction(ctx, id)
	if err != nil {
		return err
	}
	return k.abortAuction(ctx, a, reason)
}

// settleAuction finishes an ended auction: a high bid buys the token if it
// is still sellable (otherwise the bid is refunded); without bids the token
// simply unlocks. An auction ends at least auction_extension seconds before
// anything in the tree expires (StartAuction and the anti-sniping cap in
// Bid), so the settling block normally finds the token live.
func (k Keeper) settleAuction(ctx context.Context, a types.Auction) (bool, error) {
	c, err := k.getClass(ctx, a.ClassId)
	if err != nil {
		return false, err
	}
	t, err := k.getToken(ctx, a.ClassId, a.TokenId)
	if err != nil {
		return false, err
	}
	if a.HighBidder == "" || sellable(c, t, now(ctx)) != nil || k.treeSellable(ctx, t) != nil || !k.GetParams(ctx).MarketEnabled {
		reason := "no_bids"
		if a.HighBidder != "" {
			reason = "unsellable"
		}
		return false, k.abortAuction(ctx, a, reason)
	}
	// The token (or the tree under it) changed since the winning bid pinned
	// it: refund rather than sell the bidder something else.
	if !k.auctionPinsHold(ctx, a, t) {
		return false, k.abortAuction(ctx, a, "pin_mismatch")
	}
	if err := k.closeAuction(ctx, a); err != nil {
		return false, err
	}
	if t, err = k.getToken(ctx, a.ClassId, a.TokenId); err != nil {
		return false, err
	}
	s, paid, redirected, err := k.sellFromEscrow(ctx, c, &t, a.HighBidder, a.HighBid)
	if err != nil {
		return false, err
	}
	emit(ctx, types.EventAuctionSettled, classAttr(a.ClassId), tokenAttr(a.TokenId),
		sdk.NewAttribute("auction_id", u64(a.Id)), sdk.NewAttribute("sold", "true"),
		sdk.NewAttribute("seller", a.Seller), sdk.NewAttribute("buyer", a.HighBidder),
		sdk.NewAttribute("price", a.HighBid.String()), sdk.NewAttribute("fee", s.Fee.String()),
		sdk.NewAttribute("royalty", s.Royalty.String()), sdk.NewAttribute("royalty_recipients", joinParts(paid)),
		sdk.NewAttribute("royalty_redirected", boolStr(redirected)))
	return true, nil
}

// auctionPinsHold reports whether the token still matches the high bid's
// metadata and contents pins (empty pins always hold).
func (k Keeper) auctionPinsHold(ctx context.Context, a types.Auction, t types.Token) bool {
	if a.HighBidMetadataHash != "" && a.HighBidMetadataHash != types.MetadataHash(t.Metadata) {
		return false
	}
	return k.checkContentsPin(ctx, t, a.HighBidContentsHash) == nil
}

func joinParts(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}
