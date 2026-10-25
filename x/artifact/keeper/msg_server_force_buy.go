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

// ForceBuy buys a Harberger token at its current price, with or without the
// holder's consent (docs/x-artifact-spec.md §5.11.2). The price is settled
// like a sale (fee, royalty, proceeds to the holder) and the buyer starts a
// fresh tax state at the price paid. Hidden tokens and classes, and the
// market circuit breaker, stop forced sales.
func (k msgServer) ForceBuy(ctx context.Context, msg *types.MsgForceBuy) (*types.MsgForceBuyResponse, error) {
	buyer, err := k.decodeAddr("buyer", msg.Buyer)
	if err != nil {
		return nil, err
	}
	if !k.GetParams(ctx).MarketEnabled {
		return nil, types.ErrMarketDisabled
	}
	if err := k.validatePrice(ctx, msg.MaxPrice, true); err != nil {
		return nil, err
	}
	c, err := k.getClass(ctx, msg.ClassId)
	if err != nil {
		return nil, err
	}
	if !c.IsHarberger() {
		return nil, types.ErrNotHarberger
	}
	t, err := k.getToken(ctx, msg.ClassId, msg.TokenId)
	if err != nil {
		return nil, err
	}
	if isTargetHidden(c, t) {
		return nil, types.ErrContentHidden
	}
	if k.sameMember(ctx, msg.Buyer, t.Owner) {
		return nil, errorsmod.Wrap(types.ErrInvalidRecipient, "you already hold this token")
	}
	if _, err := k.validateRecipient("buyer", msg.Buyer); err != nil {
		return nil, err
	}
	if msg.ExpectedMetadataHash != "" && msg.ExpectedMetadataHash != types.MetadataHash(t.Metadata) {
		return nil, types.ErrMetadataChanged
	}
	state := types.HarbergerState{Price: math.ZeroInt()}
	if t.Harberger != nil {
		state = *t.Harberger
	}
	if !types.HarbergerForSale(state, now(ctx)) {
		return nil, types.ErrNotForSale
	}
	price, foreclosed := types.CurrentHarbergerPrice(c.Harberger, state, now(ctx))
	if price.GT(msg.MaxPrice.Amount) {
		return nil, errorsmod.Wrapf(types.ErrPriceExceeded, "price %s > max %s", price, msg.MaxPrice.Amount)
	}
	if t.Lock == types.TokenLock_TOKEN_LOCK_PENDING_TRANSFER {
		if err := k.resolvePending(ctx, t.ClassId, t.Id, OutcomeCancelled); err != nil {
			return nil, err
		}
		if t, err = k.getToken(ctx, msg.ClassId, msg.TokenId); err != nil {
			return nil, err
		}
	}
	seller := t.Owner
	s, paid, redirected := Settlement{Fee: math.ZeroInt(), Royalty: math.ZeroInt(), Proceeds: math.ZeroInt()}, []string{}, false
	if price.IsPositive() {
		if s, paid, redirected, err = k.settleSale(ctx, c, buyer, seller, price); err != nil {
			return nil, err
		}
	}
	if err := k.moveTokenAt(ctx, &t, msg.Buyer, price); err != nil {
		return nil, err
	}
	emit(ctx, types.EventTransferred, classAttr(t.ClassId), tokenAttr(t.Id),
		sdk.NewAttribute("from", seller), sdk.NewAttribute("to", msg.Buyer), sdk.NewAttribute("via", "force_buy"))
	emit(ctx, types.EventForceBought, classAttr(t.ClassId), tokenAttr(t.Id),
		sdk.NewAttribute("seller", seller), sdk.NewAttribute("buyer", msg.Buyer),
		sdk.NewAttribute("price", price.String()), sdk.NewAttribute("foreclosed", strconv.FormatBool(foreclosed)),
		sdk.NewAttribute("fee", s.Fee.String()), sdk.NewAttribute("royalty", s.Royalty.String()),
		sdk.NewAttribute("royalty_recipients", strings.Join(paid, ",")),
		sdk.NewAttribute("royalty_redirected", strconv.FormatBool(redirected)))
	return &types.MsgForceBuyResponse{Price: price.String(), Fee: s.Fee.String(), Royalty: s.Royalty.String(), Proceeds: s.Proceeds.String()}, nil
}
