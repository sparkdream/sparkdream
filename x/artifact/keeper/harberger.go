package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// Harberger tokens (docs/x-artifact-spec.md §7.24). The holder self-assesses
// a price and pays a yearly tax on it through an x/session RecurringPull
// granted to the artifact module; anyone may buy the token at that price at
// any time. A holder with no tax grant once the grace period ends, or whose
// pull fails, is foreclosed: the price falls linearly to zero over the
// class's foreclosure_duration. The EndBlocker takes each due pull and
// sends it on to the community pool.

func (k Keeper) indexHarbergerDue(ctx context.Context, t types.Token) error {
	if t.Harberger == nil || t.Harberger.NextDue == 0 {
		return nil
	}
	return k.HarbergerDue.Set(ctx, collections.Join3(t.Harberger.NextDue, t.ClassId, t.Id))
}

func (k Keeper) unindexHarbergerDue(ctx context.Context, t types.Token) error {
	if t.Harberger == nil || t.Harberger.NextDue == 0 {
		return nil
	}
	return k.HarbergerDue.Remove(ctx, collections.Join3(t.Harberger.NextDue, t.ClassId, t.Id))
}

// resetHarberger restarts a Harberger token's tax state for a buyer:
// priced at what they paid, no grant, one tax period of grace. Gifts carry
// the old state instead (types.GiftedHarbergerState).
func (k Keeper) resetHarberger(ctx context.Context, c types.Class, t *types.Token, price math.Int) error {
	if !c.IsHarberger() {
		return nil
	}
	if err := k.unindexHarbergerDue(ctx, *t); err != nil {
		return err
	}
	t.Harberger = types.NewHarbergerState(c.Harberger, price, now(ctx))
	return nil
}

// validateTaxGrant checks that grantID is a RecurringPull from owner to the
// artifact module paying at least the tax on price every tax period, and
// returns when its next pull is due.
func (k Keeper) validateTaxGrant(ctx context.Context, c types.Class, owner string, price math.Int, grantID uint64) (int64, error) {
	if k.late.session == nil {
		return 0, errorsmod.Wrap(types.ErrInvalidHarbergerGrant, "x/session not wired")
	}
	info, found, err := k.late.session.RecurringPull(ctx, grantID)
	if err != nil {
		return 0, errorsmod.Wrap(types.ErrInvalidHarbergerGrant, err.Error())
	}
	if !found {
		return 0, errorsmod.Wrapf(types.ErrInvalidHarbergerGrant, "grant %d is not a RecurringPull", grantID)
	}
	tax := c.Harberger.TaxPerPeriod(price)
	switch {
	case !k.canActAs(ctx, owner, info.Granter):
		return 0, errorsmod.Wrap(types.ErrInvalidHarbergerGrant, "the grant must come from the holder")
	case info.Grantee != k.ModuleAddress().String():
		return 0, errorsmod.Wrapf(types.ErrInvalidHarbergerGrant, "the grantee must be the artifact module %s", k.ModuleAddress())
	case !info.Active:
		return 0, errorsmod.Wrap(types.ErrInvalidHarbergerGrant, "the grant is not active")
	case info.Amount.Denom != k.BondDenom(ctx) || info.Amount.Amount.LT(tax):
		return 0, errorsmod.Wrapf(types.ErrInvalidHarbergerGrant, "the grant must pay at least %s%s per period", tax, k.BondDenom(ctx))
	case info.PeriodSeconds != c.Harberger.TaxPeriod:
		return 0, errorsmod.Wrapf(types.ErrInvalidHarbergerGrant, "the grant period must be %d seconds", c.Harberger.TaxPeriod)
	case info.NextClaim >= info.ExpiresAt:
		return 0, errorsmod.Wrap(types.ErrInvalidHarbergerGrant, "the grant ends before its first pull")
	case info.NextClaim > now(ctx)+c.Harberger.TaxPeriod:
		return 0, errorsmod.Wrap(types.ErrInvalidHarbergerGrant, "the grant's first pull must fall within one tax period")
	case !info.MaxPerEpoch.IsNil() && info.MaxPerEpoch.IsPositive() && info.MaxPerEpoch.LT(info.Amount.Amount.MulRaw(2)):
		// A pull delayed into the UTC day of the next one must not trip
		// session's per-day ceiling (ART-204).
		return 0, errorsmod.Wrapf(types.ErrInvalidHarbergerGrant, "the grant's max_per_epoch must cover two pulls (%s%s)", info.Amount.Amount.MulRaw(2), k.BondDenom(ctx))
	}
	return info.NextClaim, nil
}

// collectTax takes one due pull of a token's tax grant, inside its own
// cached context so a failed pull leaves session state untouched, and
// passes it on to the community pool. It returns what was paid.
func (k Keeper) collectTax(ctx sdk.Context, grantID uint64) (math.Int, int64, error) {
	if k.late.session == nil {
		return math.ZeroInt(), 0, fmt.Errorf("x/session not wired")
	}
	cacheCtx, write := ctx.CacheContext()
	coin, err := k.late.session.ClaimRecurringPull(cacheCtx, grantID)
	if errors.Is(err, types.ErrTaxPullFailed) {
		// Keep session's own transition (paused or completed grant).
		write()
		return math.ZeroInt(), 0, err
	} else if err != nil {
		return math.ZeroInt(), 0, err
	}
	if coin.Denom != k.BondDenom(ctx) {
		return math.ZeroInt(), 0, fmt.Errorf("tax grant paid %s, not the bond denom", coin.Denom)
	}
	if err := k.fundCommunityPool(cacheCtx, k.ModuleAddress(), coin.Amount); err != nil {
		return math.ZeroInt(), 0, err
	}
	info, found, err := k.late.session.RecurringPull(cacheCtx, grantID)
	if err != nil || !found || !info.Active || info.NextClaim >= info.ExpiresAt {
		// Paid this period, but the grant cannot pay the next one.
		write()
		return coin.Amount, 0, nil
	}
	write()
	return coin.Amount, info.NextClaim, nil
}

// collectHarbergerTax is the EndBlocker pass that takes due tax pulls. A
// pull that fails forecloses the token from its due time; a grant with no
// further pull forecloses it one period later; a pull session defers
// (ErrTaxPullDeferred) is retried when the UTC day turns.
func (k Keeper) collectHarbergerTax(ctx sdk.Context, cutoff int64, limit int) {
	for _, key := range dueTimeTokens(ctx, k.HarbergerDue, cutoff, limit) {
		classID, tokenID := key.K2(), key.K3()
		k.safely(ctx, "harberger", fmt.Sprintf("%d/%d", classID, tokenID), func(c sdk.Context) error {
			if err := k.HarbergerDue.Remove(c, key); err != nil {
				return err
			}
			t, err := k.getToken(c, classID, tokenID)
			if err != nil || t.Harberger == nil || t.Harberger.GrantId == 0 || t.Harberger.NextDue != key.K1() {
				return nil // stale entry
			}
			h := t.Harberger
			paid, next, err := k.collectTax(c, h.GrantId)
			if errors.Is(err, types.ErrTaxPullDeferred) {
				// A passing condition on session's side (its per-day
				// ceiling): try again when the day turns, no foreclosure
				// (ART-204).
				h.NextDue = nextUTCDay(key.K1())
				emit(c, types.EventHarbergerTaxDeferred, classAttr(classID), tokenAttr(tokenID),
					sdk.NewAttribute("holder", t.Owner), sdk.NewAttribute("error", err.Error()),
					sdk.NewAttribute("next_due", i64(h.NextDue)))
				if err := k.saveToken(c, t); err != nil {
					return err
				}
				return k.indexHarbergerDue(c, t)
			}
			if err != nil {
				h.ForeclosedAt = key.K1()
				h.GrantId, h.NextDue = 0, 0
				emit(c, types.EventHarbergerForeclosed, classAttr(classID), tokenAttr(tokenID),
					sdk.NewAttribute("holder", t.Owner), sdk.NewAttribute("error", err.Error()))
				return k.saveToken(c, t)
			}
			h.TaxPaid = h.TaxPaid.Add(paid)
			// The period this pull pays for is held at the current price.
			h.PaidPrice = h.Price
			if next == 0 {
				// The grant is spent: the holder has until the end of the
				// period it paid for to name a new one.
				h.GrantId, h.NextDue = 0, 0
				h.GraceUntil = key.K1() + k.harbergerPeriod(c, classID)
			} else {
				h.NextDue = next
			}
			emit(c, types.EventHarbergerTaxPaid, classAttr(classID), tokenAttr(tokenID),
				sdk.NewAttribute("holder", t.Owner), sdk.NewAttribute("amount", paid.String()),
				sdk.NewAttribute("next_due", i64(h.NextDue)))
			if err := k.saveToken(c, t); err != nil {
				return err
			}
			return k.indexHarbergerDue(c, t)
		}, func() {
			// Re-queue the token one period later rather than dropping its
			// due entry, which would stop tax (and foreclosure) for good.
			if !k.tryCached(ctx, func(c sdk.Context) error {
				if err := k.HarbergerDue.Remove(c, key); err != nil {
					return err
				}
				t, err := k.getToken(c, classID, tokenID)
				if err != nil || t.Harberger == nil || t.Harberger.NextDue != key.K1() {
					return nil
				}
				period := k.harbergerPeriod(c, classID)
				if period <= 0 {
					return fmt.Errorf("class %d has no tax period", classID)
				}
				t.Harberger.NextDue = key.K1() + period
				if err := k.saveToken(c, t); err != nil {
					return err
				}
				return k.indexHarbergerDue(c, t)
			}) {
				if err := k.HarbergerDue.Remove(ctx, key); err != nil {
					ctx.Logger().Error("collectHarbergerTax: HarbergerDue.Remove failed", "err", err)
				}
			}
		})
	}
}

// nextUTCDay is the first second of the UTC day after t, when session's
// per-grant epoch bucket resets.
func nextUTCDay(t int64) int64 {
	return (t/86400 + 1) * 86400
}

func (k Keeper) harbergerPeriod(ctx context.Context, classID uint64) int64 {
	c, err := k.getClass(ctx, classID)
	if err != nil {
		return 0
	}
	return c.Harberger.TaxPeriod
}
