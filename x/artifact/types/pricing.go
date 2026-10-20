package types

import (
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
)

// Public mint price curves (docs/x-artifact-spec.md §7.18).

// ValidatePricing checks the curve fields of a mint policy. The price denom
// and window are checked by the keeper.
func ValidatePricing(mp MintPolicy) error {
	if _, ok := MintPricing_name[int32(mp.Pricing)]; !ok {
		return errorsmod.Wrapf(ErrInvalidMintPricing, "unknown pricing %d", mp.Pricing)
	}
	step := mp.PriceStep
	floor := mp.FloorPrice
	switch mp.Pricing {
	case MintPricing_MINT_PRICING_FIXED:
		if (!step.IsNil() && !step.IsZero()) || (!floor.IsNil() && !floor.IsZero()) {
			return errorsmod.Wrap(ErrInvalidMintPricing, "price_step and floor_price apply only to curves")
		}
	case MintPricing_MINT_PRICING_LINEAR_SUPPLY:
		if step.IsNil() || !step.IsPositive() {
			return errorsmod.Wrap(ErrInvalidMintPricing, "LINEAR_SUPPLY needs a positive price_step")
		}
		if !floor.IsNil() && !floor.IsZero() {
			return errorsmod.Wrap(ErrInvalidMintPricing, "floor_price applies only to DUTCH")
		}
	case MintPricing_MINT_PRICING_DUTCH:
		if mp.StartTime <= 0 || mp.EndTime <= 0 {
			return errorsmod.Wrap(ErrInvalidMintPricing, "DUTCH needs start_time and end_time")
		}
		if floor.IsNil() || floor.IsNegative() {
			return errorsmod.Wrap(ErrInvalidMintPricing, "DUTCH needs a non-negative floor_price")
		}
		if mp.Price.Amount.IsNil() || floor.GT(mp.Price.Amount) {
			return errorsmod.Wrap(ErrInvalidMintPricing, "floor_price must not exceed price")
		}
		if !step.IsNil() && !step.IsZero() {
			return errorsmod.Wrap(ErrInvalidMintPricing, "price_step applies only to LINEAR_SUPPLY")
		}
	}
	return nil
}

// NormalizePricing fills nil Int fields with zero so stored policies compare.
func NormalizePricing(mp MintPolicy) MintPolicy {
	if mp.PriceStep.IsNil() {
		mp.PriceStep = math.ZeroInt()
	}
	if mp.FloorPrice.IsNil() {
		mp.FloorPrice = math.ZeroInt()
	}
	return mp
}

// UnitPrice is the price of the k-th public mint of a class (publicMinted
// tokens sold so far, 0-based) at block time now.
func UnitPrice(mp MintPolicy, publicMinted uint64, now int64) math.Int {
	base := mp.Price.Amount
	if base.IsNil() {
		base = math.ZeroInt()
	}
	switch mp.Pricing {
	case MintPricing_MINT_PRICING_LINEAR_SUPPLY:
		return base.Add(mp.PriceStep.Mul(math.NewIntFromUint64(publicMinted)))
	case MintPricing_MINT_PRICING_DUTCH:
		if now <= mp.StartTime {
			return base
		}
		if now >= mp.EndTime {
			return mp.FloorPrice
		}
		// base - (base - floor) * elapsed / window, floor division on the drop.
		drop := base.Sub(mp.FloorPrice).MulRaw(now - mp.StartTime).QuoRaw(mp.EndTime - mp.StartTime)
		return base.Sub(drop)
	default:
		return base
	}
}

// BatchPrice totals quantity public mints starting at publicMinted and
// returns the total and the most expensive unit (what max_price must cover).
func BatchPrice(mp MintPolicy, publicMinted uint64, quantity uint32, now int64) (total, maxUnit math.Int) {
	total = math.ZeroInt()
	maxUnit = math.ZeroInt()
	for i := uint32(0); i < quantity; i++ {
		u := UnitPrice(mp, publicMinted+uint64(i), now)
		total = total.Add(u)
		if u.GT(maxUnit) {
			maxUnit = u
		}
	}
	return total, maxUnit
}

// CurveOpened reports whether the public mint curve is fixed: public mints
// have happened, or the window is open (enabled and start_time reached).
func CurveOpened(c Class, now int64) bool {
	if c.PublicMinted > 0 {
		return true
	}
	return c.MintPolicy.PublicMintEnabled && c.MintPolicy.StartTime <= now
}

// SameCurve reports whether two policies agree on every pricing field.
func SameCurve(a, b MintPolicy) bool {
	a, b = NormalizePricing(a), NormalizePricing(b)
	return a.Pricing == b.Pricing && a.Price.Equal(b.Price) && a.PriceStep.Equal(b.PriceStep) &&
		a.FloorPrice.Equal(b.FloorPrice) && a.StartTime == b.StartTime && a.EndTime == b.EndTime
}
