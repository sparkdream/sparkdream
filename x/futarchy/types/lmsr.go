package types

import (
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Precision constants
var (
	// Epsilon is the series cut-off. With the range reductions below every
	// series argument is bounded (|r| <= 1 for Exp, |y| <= 1/3 for Ln), so
	// the series reach the 18-decimal resolution of LegacyDec in about 20
	// terms; stopping at the last representable digit keeps the LMSR math
	// exact to rounding instead of 1e-8 * b.
	Epsilon       = math.LegacyMustNewDecFromStr("0.000000000000000001")
	MaxIterations = 100

	// Charge 200 Gas per iteration (adjust based on benchmark)
	LmsrIterationGasCost = uint64(200)

	// DefaultMaxExponent is the fallback when params are unavailable (e.g., genesis).
	// Callers should prefer passing the MaxLmsrExponent param from module params.
	DefaultMaxExponent = math.LegacyNewDec(20)

	// Ln2 is ln(2) to the full 18-decimal precision of LegacyDec. It is used
	// for b = liquidity / ln(2) (so b*ln(2) == liquidity to rounding) and for
	// the range reduction in Ln.
	Ln2 = math.LegacyMustNewDecFromStr("0.693147180559945309")

	// maxExpArg bounds the argument of Exp. Every caller passes a
	// non-positive exponent (the log-sum-exp forms below subtract the
	// maximum first); the bound only guards LegacyDec against overflow if a
	// future caller passes a large positive value. e^100 ~ 2.7e43 fits.
	maxExpArg = math.LegacyNewDec(100)

	half = math.LegacyMustNewDecFromStr("0.5")
)

// CheckPriceBound enforces max_lmsr_exponent as a bound on how far the
// market may be pushed (FUT-03): |qYes - qNo| / b must stay <= maxExp, so the
// implied price stays within [1/(1+e^maxExp), e^maxExp/(1+e^maxExp)]. It
// replaces the old absolute-exponent clamp, which flattened the cost
// function past the bound and mispriced trades and settlements. A trade that
// would cross the bound is rejected instead.
func CheckPriceBound(b, qYes, qNo, maxExp math.LegacyDec) error {
	if b.LTE(math.LegacyZeroDec()) {
		return fmt.Errorf("b must be positive, got %s", b.String())
	}
	if qYes.Sub(qNo).Abs().Quo(b).GT(maxExp) {
		return fmt.Errorf("trade would push |q_yes - q_no| / b beyond max_lmsr_exponent %s", maxExp.String())
	}
	return nil
}

// CalculateLMSRCost calculates the LMSR cost function
//
//	C(qYes, qNo) = b * ln(e^(qYes/b) + e^(qNo/b))
//	             = b * (max + ln(1 + e^(min - max)))
//
// in the log-sum-exp form, so no exponent is ever positive and nothing is
// clamped: the result is exact for any pool state.
// Returns an error instead of panicking on invalid inputs (e.g. b <= 0).
func CalculateLMSRCost(ctx sdk.Context, b math.LegacyDec, qYes, qNo math.LegacyDec) (math.LegacyDec, error) {
	if b.LTE(math.LegacyZeroDec()) {
		return math.LegacyZeroDec(), fmt.Errorf("CalculateLMSRCost: b must be positive, got %s", b.String())
	}

	x := qYes.Quo(b)
	y := qNo.Quo(b)
	hi, lo := x, y
	if y.GT(x) {
		hi, lo = y, x
	}

	lnSum, err := Ln(ctx, math.LegacyOneDec().Add(Exp(ctx, lo.Sub(hi))))
	if err != nil {
		return math.LegacyZeroDec(), fmt.Errorf("CalculateLMSRCost: ln failed: %w", err)
	}
	return b.Mul(hi.Add(lnSum)), nil
}

// SharesForAmount returns the new pool value and the shares minted when
// `amount` (after fees) is spent on the YES (isYes) or NO side:
//
//	q_side' = C' + b * ln(1 - e^((q_other - C') / b)),  C' = C(q) + amount
//
// (q_other - C') / b is always negative because C' > C >= q_other, so the
// exponent is safe.
func SharesForAmount(ctx sdk.Context, b, qYes, qNo, amount math.LegacyDec, isYes bool) (newPool, sharesOut math.LegacyDec, err error) {
	currentCost, err := CalculateLMSRCost(ctx, b, qYes, qNo)
	if err != nil {
		return math.LegacyZeroDec(), math.LegacyZeroDec(), err
	}
	newCost := currentCost.Add(amount)

	side, other := qYes, qNo
	if !isYes {
		side, other = qNo, qYes
	}
	oneMinus := math.LegacyOneDec().Sub(Exp(ctx, other.Sub(newCost).Quo(b)))
	if oneMinus.LTE(math.LegacyZeroDec()) {
		return math.LegacyZeroDec(), math.LegacyZeroDec(), fmt.Errorf("trade too large: would deplete market liquidity")
	}
	lnTerm, err := Ln(ctx, oneMinus)
	if err != nil {
		return math.LegacyZeroDec(), math.LegacyZeroDec(), err
	}
	newPool = newCost.Add(b.Mul(lnTerm))
	return newPool, newPool.Sub(side), nil
}

// Exp computes e^x with range reduction: x is halved k times until
// |x| <= 1, the Taylor series is evaluated on the reduced argument (fast and
// convergent), and the result is squared back k times. The plain Taylor
// series diverged for large |x| (Exp(-60) evaluated to -4.4e19, FUT-04).
// Consumes gas per loop.
func Exp(ctx sdk.Context, x math.LegacyDec) math.LegacyDec {
	if x.IsZero() {
		return math.LegacyOneDec()
	}
	if x.GT(maxExpArg) {
		x = maxExpArg
	}

	one := math.LegacyOneDec()
	r := x
	k := 0
	for r.Abs().GT(one) {
		r = r.QuoInt64(2)
		k++
	}

	result := math.LegacyOneDec()
	term := math.LegacyOneDec()
	for i := 1; i < MaxIterations; i++ {
		// SAFETY: Consume Gas
		ctx.GasMeter().ConsumeGas(LmsrIterationGasCost, "lmsr_exp_iteration")

		term = term.Mul(r).QuoInt64(int64(i))
		result = result.Add(term)

		if term.Abs().LT(Epsilon) {
			break
		}
	}

	for i := 0; i < k; i++ {
		ctx.GasMeter().ConsumeGas(LmsrIterationGasCost, "lmsr_exp_iteration")
		result = result.Mul(result)
	}
	return result
}

// Ln computes the natural logarithm. Returns an error if x <= 0 instead of
// panicking. Range reduction: x = m * 2^k with m in [1, 2), so
// ln(x) = k*ln(2) + ln(m), and the atanh series for ln(m) has
// |y| = |(m-1)/(m+1)| <= 1/3 and converges in about 20 terms. Without the
// reduction the series was badly truncated for x near 0 or very large x.
func Ln(ctx sdk.Context, x math.LegacyDec) (math.LegacyDec, error) {
	if x.LTE(math.LegacyZeroDec()) {
		return math.LegacyZeroDec(), fmt.Errorf("Ln undefined for x <= 0, got %s", x.String())
	}

	two := math.LegacyNewDec(2)
	one := math.LegacyOneDec()
	m := x
	k := int64(0)
	for m.GTE(two) {
		m = m.QuoInt64(2)
		k++
	}
	for m.LT(one) {
		m = m.MulInt64(2)
		k--
	}

	result := math.LegacyZeroDec()
	y := m.Sub(one).Quo(m.Add(one))
	ySquared := y.Mul(y)
	term := y

	for i := 0; i < MaxIterations; i++ {
		// SAFETY: Consume Gas
		ctx.GasMeter().ConsumeGas(LmsrIterationGasCost, "lmsr_ln_iteration")

		addToResult := term.QuoInt64(int64(2*i + 1))
		result = result.Add(addToResult)

		term = term.Mul(ySquared)

		if addToResult.Abs().LT(Epsilon) {
			break
		}
	}

	return result.MulInt64(2).Add(Ln2.MulInt64(k)), nil
}

// SettlementPriceYes returns p_yes = exp(qYes/b) / (exp(qYes/b) + exp(qNo/b)),
// the LMSR-implied probability of YES at the given pool state. Returns 1/2
// when both pools are zero (no information, fair price). Computed as the
// logistic of d = (qYes - qNo)/b with a non-positive exponent, so it is
// exact for any pool state (no clamp: FUT-03).
//
// Used by CancelMarket and RESOLVED_INVALID resolution to snapshot a fair
// price at which holders can redeem their YES/NO shares (FUTARCHY-S2-1), and
// by the time-weighted resolution price (FUT-01).
func SettlementPriceYes(ctx sdk.Context, b, qYes, qNo math.LegacyDec) (math.LegacyDec, error) {
	if b.LTE(math.LegacyZeroDec()) {
		return math.LegacyZeroDec(), fmt.Errorf("SettlementPriceYes: b must be positive, got %s", b.String())
	}
	if qYes.Equal(qNo) {
		// Symmetry: with no information, both outcomes are equally likely.
		return half, nil
	}

	d := qYes.Sub(qNo).Quo(b)
	if d.IsPositive() {
		// p = 1 / (1 + e^-d)
		return math.LegacyOneDec().Quo(math.LegacyOneDec().Add(Exp(ctx, d.Neg()))), nil
	}
	// p = e^d / (1 + e^d)
	e := Exp(ctx, d)
	return e.Quo(math.LegacyOneDec().Add(e)), nil
}

// softplus returns ln(1 + e^z) in the stable form: for z > 0 it is
// z + ln(1 + e^-z), so the exponent is never positive.
func softplus(ctx sdk.Context, z math.LegacyDec) (math.LegacyDec, error) {
	if z.IsPositive() {
		l, err := Ln(ctx, math.LegacyOneDec().Add(Exp(ctx, z.Neg())))
		if err != nil {
			return math.LegacyZeroDec(), err
		}
		return z.Add(l), nil
	}
	return Ln(ctx, math.LegacyOneDec().Add(Exp(ctx, z)))
}

// CreatorResidualResolved returns the LMSR remainder after a YES/NO winner
// claims 1 spark per winning share:
//
//	residual = C(q) - qWinner = b * ln(1 + exp((qLoser - qWinner) / b))
//
// This is the creator's correct WithdrawLiquidity payout for RESOLVED_YES and
// RESOLVED_NO markets (FUTARCHY-S2-1). When the winner holds at least as
// many shares as the loser it lies between 0 and InitialLiquidity = b*ln(2).
// Under the time-weighted resolution (FUT-01) the winner can be the side with
// FEWER outstanding shares; the residual then exceeds InitialLiquidity (the
// traders who bet on the losing side paid for it) and is still fully backed
// because the module holds C(q) for the market.
func CreatorResidualResolved(ctx sdk.Context, b, qWinner, qLoser math.LegacyDec) (math.LegacyDec, error) {
	if b.LTE(math.LegacyZeroDec()) {
		return math.LegacyZeroDec(), fmt.Errorf("CreatorResidualResolved: b must be positive")
	}
	sp, err := softplus(ctx, qLoser.Sub(qWinner).Quo(b))
	if err != nil {
		return math.LegacyZeroDec(), fmt.Errorf("CreatorResidualResolved: ln failed: %w", err)
	}
	return b.Mul(sp), nil
}

// CreatorResidualSettled returns the LMSR market-maker's residual after every
// outstanding YES/NO share is redeemed at LMSR-implied prices (the
// CANCELLED / RESOLVED_INVALID payout model):
//
//	residual = b * H(p_yes) where H(p) = -p*ln(p) - (1-p)*ln(1-p)
//
// This is the entropy of the implied probability distribution scaled by b.
// At p=1/2: residual = b*ln(2) = InitialLiquidity (no information lost).
// At p→0 or p→1: residual → 0 (creator's full subsidy paid out as price
// discovery). Mathematically equivalent to C(qYes,qNo) - qYes*p_yes - qNo*p_no.
func CreatorResidualSettled(ctx sdk.Context, b, pYes math.LegacyDec) (math.LegacyDec, error) {
	if b.LTE(math.LegacyZeroDec()) {
		return math.LegacyZeroDec(), fmt.Errorf("CreatorResidualSettled: b must be positive")
	}
	if pYes.LTE(math.LegacyZeroDec()) || pYes.GTE(math.LegacyOneDec()) {
		// Limit cases: H(0) = H(1) = 0 (no entropy, full subsidy paid out).
		return math.LegacyZeroDec(), nil
	}
	pNo := math.LegacyOneDec().Sub(pYes)
	lnYes, err := Ln(ctx, pYes)
	if err != nil {
		return math.LegacyZeroDec(), fmt.Errorf("CreatorResidualSettled: ln(p_yes) failed: %w", err)
	}
	lnNo, err := Ln(ctx, pNo)
	if err != nil {
		return math.LegacyZeroDec(), fmt.Errorf("CreatorResidualSettled: ln(p_no) failed: %w", err)
	}
	// H = -p*ln(p) - (1-p)*ln(1-p) — both terms positive since ln(p<1) < 0.
	entropy := pYes.Mul(lnYes).Neg().Sub(pNo.Mul(lnNo))
	return b.Mul(entropy), nil
}
