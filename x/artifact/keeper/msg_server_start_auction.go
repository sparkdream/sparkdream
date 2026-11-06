package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// StartAuction puts a token up for an English auction
// (docs/x-artifact-spec.md §5.13.3). The token is AUCTION-locked until the
// auction settles or is cancelled.
func (k msgServer) StartAuction(ctx context.Context, msg *types.MsgStartAuction) (*types.MsgStartAuctionResponse, error) {
	if _, err := k.decodeAddr("seller", msg.Seller); err != nil {
		return nil, err
	}
	p := k.GetParams(ctx)
	if !p.MarketEnabled {
		return nil, types.ErrMarketDisabled
	}
	c, err := k.getClass(ctx, msg.ClassId)
	if err != nil {
		return nil, err
	}
	if err := marketable(c); err != nil {
		return nil, err
	}
	t, err := k.getToken(ctx, msg.ClassId, msg.TokenId)
	if err != nil {
		return nil, err
	}
	if !k.canActAs(ctx, msg.Seller, t.Owner) {
		return nil, types.ErrNotTokenOwner
	}
	if t.Lock != types.TokenLock_TOKEN_LOCK_NONE {
		return nil, errorsmod.Wrapf(types.ErrTokenLocked, "token is %s", t.Lock)
	}
	if err := sellable(c, t, now(ctx)); err != nil {
		return nil, err
	}
	if err := k.treeSellable(ctx, t); err != nil {
		return nil, err
	}
	if err := k.validatePrice(ctx, msg.ReservePrice, false); err != nil {
		return nil, err
	}
	if msg.MinIncrementBps > 10000 {
		return nil, errorsmod.Wrap(types.ErrBidTooLow, "min_increment_bps must be at most 10000")
	}
	if msg.Duration <= 0 || msg.Duration > p.MaxListingDuration {
		return nil, errorsmod.Wrapf(types.ErrInvalidMetadata, "duration must be in (0, %d] seconds", p.MaxListingDuration)
	}
	// The auction must end at least auction_extension seconds before
	// anything in the tree expires, so the settling block finds a live
	// token (the anti-sniping extension is capped the same way in Bid).
	expiry, err := k.treeExpiry(ctx, t)
	if err != nil {
		return nil, err
	}
	if expiry > 0 && now(ctx)+msg.Duration > expiry-p.AuctionExtension {
		return nil, errorsmod.Wrapf(types.ErrTokenExpired,
			"the auction must end at least %d seconds before the token expires; renew it first", p.AuctionExtension)
	}
	seq, err := k.AuctionSeq.Next(ctx)
	if err != nil {
		return nil, err
	}
	a := types.Auction{
		Id:              seq + 1,
		ClassId:         t.ClassId,
		TokenId:         t.Id,
		Seller:          t.Owner, // the stored owner, which a migrated signer acts for (ART-102)
		ReservePrice:    msg.ReservePrice,
		MinIncrementBps: msg.MinIncrementBps,
		CreatedAt:       now(ctx),
		EndsAt:          now(ctx) + msg.Duration,
		HighBid:         math.ZeroInt(),
	}
	if err := k.clearRentalOffer(ctx, t.ClassId, t.Id, "auctioned"); err != nil {
		return nil, err
	}
	t.Lock = types.TokenLock_TOKEN_LOCK_AUCTION
	if err := k.saveToken(ctx, t); err != nil {
		return nil, err
	}
	if err := k.Auctions.Set(ctx, a.Id, a); err != nil {
		return nil, err
	}
	if err := k.AuctionByToken.Set(ctx, collections.Join(a.ClassId, a.TokenId), a.Id); err != nil {
		return nil, err
	}
	if err := k.AuctionEnds.Set(ctx, collections.Join(a.EndsAt, a.Id)); err != nil {
		return nil, err
	}
	if err := k.AuctionsBySeller.Set(ctx, collections.Join(a.Seller, a.Id)); err != nil {
		return nil, err
	}
	emit(ctx, types.EventAuctionStarted, classAttr(a.ClassId), tokenAttr(a.TokenId),
		sdk.NewAttribute("auction_id", u64(a.Id)), sdk.NewAttribute("seller", a.Seller),
		sdk.NewAttribute("reserve_price", a.ReservePrice.String()), sdk.NewAttribute("ends_at", i64(a.EndsAt)))
	return &types.MsgStartAuctionResponse{AuctionId: a.Id}, nil
}
