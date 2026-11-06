package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// SetHarbergerPrice self-assesses a Harberger token's price
// (docs/x-artifact-spec.md §5.11.1). A positive price needs a RecurringPull
// from the holder to the artifact module paying at least the tax on it
// each tax period; setting one also cures a foreclosure. Time between
// what is paid through and the grant's first pull is charged up front, at
// the higher of the price held so far and the new one, and raising the
// price within the time already paid for costs the difference
// (types.HarbergerState.ReassessmentCharge). A zero price abandons the
// token: once its grace period ends anyone may take it free.
func (k msgServer) SetHarbergerPrice(ctx context.Context, msg *types.MsgSetHarbergerPrice) (*types.MsgSetHarbergerPriceResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
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
	if !k.canActAs(ctx, msg.Owner, t.Owner) {
		return nil, types.ErrNotTokenOwner
	}
	if msg.Price.IsNil() || msg.Price.IsNegative() {
		return nil, errorsmod.Wrap(types.ErrInvalidDenom, "price must be non-negative")
	}
	if t.Harberger == nil {
		t.Harberger = types.NewHarbergerState(c.Harberger, msg.Price, now(ctx))
	}
	nextDue := int64(0)
	if msg.Price.IsPositive() {
		if nextDue, err = k.validateTaxGrant(ctx, c, msg.Owner, msg.Price, msg.GrantId); err != nil {
			return nil, err
		}
	} else if msg.GrantId != 0 {
		return nil, errorsmod.Wrap(types.ErrInvalidHarbergerGrant, "a zero price takes no tax grant")
	}
	// Nothing is held for less than the tax on it: the time the new grant
	// skips is charged at the higher of the old and new price, and a raise
	// within the time already paid for costs the difference (ART-101,
	// ART-201).
	arrears, paidPrice := t.Harberger.ReassessmentCharge(c.Harberger, msg.Price, nextDue, now(ctx))
	if arrears.IsPositive() {
		signer, err := k.decodeAddr("owner", msg.Owner)
		if err != nil {
			return nil, err
		}
		if err := k.fundCommunityPool(ctx, signer, arrears); err != nil {
			return nil, err
		}
	}
	if err := k.unindexHarbergerDue(ctx, t); err != nil {
		return nil, err
	}
	h := t.Harberger
	if arrears.IsPositive() {
		if h.TaxPaid.IsNil() {
			h.TaxPaid = math.ZeroInt()
		}
		h.TaxPaid = h.TaxPaid.Add(arrears)
	}
	h.Price = msg.Price
	h.PaidPrice = paidPrice
	h.GrantId = msg.GrantId
	h.NextDue = nextDue
	if msg.GrantId != 0 {
		h.ForeclosedAt = 0
	}
	if err := k.saveToken(ctx, t); err != nil {
		return nil, err
	}
	if err := k.indexHarbergerDue(ctx, t); err != nil {
		return nil, err
	}
	tax := c.Harberger.TaxPerPeriod(msg.Price)
	emit(ctx, types.EventHarbergerPriceSet, classAttr(c.Id), tokenAttr(t.Id),
		sdk.NewAttribute("holder", msg.Owner), sdk.NewAttribute("price", msg.Price.String()),
		sdk.NewAttribute("grant_id", u64(msg.GrantId)), sdk.NewAttribute("tax_per_period", tax.String()),
		sdk.NewAttribute("next_due", i64(nextDue)), sdk.NewAttribute("arrears", arrears.String()))
	return &types.MsgSetHarbergerPriceResponse{TaxPerPeriod: tax.String()}, nil
}
