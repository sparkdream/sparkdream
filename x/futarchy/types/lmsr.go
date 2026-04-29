package types

import (
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Precision constants
var (
	Epsilon       = math.LegacyMustNewDecFromStr("0.00000001")
	MaxIterations = 100

	// Charge 200 Gas per iteration (adjust based on benchmark)
	LmsrIterationGasCost = uint64(200)

	// DefaultMaxExponent is the fallback when params are unavailable (e.g., genesis).
	// Callers should prefer passing the MaxLmsrExponent param from module params.
	DefaultMaxExponent = math.LegacyNewDec(20)
)

// ClampExponent clamps an exponent value to prevent numerical overflow
func ClampExponent(x math.LegacyDec, maxExp math.LegacyDec) math.LegacyDec {
	if x.GT(maxExp) {
		return maxExp
	}
	negMaxExp := maxExp.Neg()
	if x.LT(negMaxExp) {
		return negMaxExp
	}
	return x
}

// CalculateLMSRCost calculates the LMSR cost function.
// maxExp controls the exponent clamp bound (use params.MaxLmsrExponent).
// Returns an error instead of panicking on invalid inputs (e.g. b <= 0).
func CalculateLMSRCost(ctx sdk.Context, b math.LegacyDec, qYes, qNo math.LegacyDec, maxExp math.LegacyDec) (math.LegacyDec, error) {
	// Validate b is positive
	if b.LTE(math.LegacyZeroDec()) {
		return math.LegacyZeroDec(), fmt.Errorf("CalculateLMSRCost: b must be positive, got %s", b.String())
	}

	x := qYes.Quo(b)
	y := qNo.Quo(b)

	// Clamp exponents for numerical stability
	x = ClampExponent(x, maxExp)
	y = ClampExponent(y, maxExp)

	max := x
	if y.GT(x) {
		max = y
	}

	// Pass ctx to Exp
	term1 := Exp(ctx, x.Sub(max))
	term2 := Exp(ctx, y.Sub(max))

	sum := term1.Add(term2)

	// Pass ctx to Ln
	lnSum, err := Ln(ctx, sum)
	if err != nil {
		return math.LegacyZeroDec(), fmt.Errorf("CalculateLMSRCost: ln failed: %w", err)
	}
	result := b.Mul(max.Add(lnSum))

	return result, nil
}

// Exp consumes gas per loop
func Exp(ctx sdk.Context, x math.LegacyDec) math.LegacyDec {
	if x.IsZero() {
		return math.LegacyOneDec()
	}

	result := math.LegacyOneDec()
	term := math.LegacyOneDec()

	for i := 1; i < MaxIterations; i++ {
		// SAFETY: Consume Gas
		ctx.GasMeter().ConsumeGas(LmsrIterationGasCost, "lmsr_exp_iteration")

		term = term.Mul(x).Quo(math.LegacyNewDec(int64(i)))
		result = result.Add(term)

		if term.Abs().LT(Epsilon) {
			break
		}
	}
	return result
}

// Ln computes the natural logarithm. Returns an error if x <= 0 instead of panicking.
func Ln(ctx sdk.Context, x math.LegacyDec) (math.LegacyDec, error) {
	if x.LTE(math.LegacyZeroDec()) {
		return math.LegacyZeroDec(), fmt.Errorf("Ln undefined for x <= 0, got %s", x.String())
	}

	result := math.LegacyZeroDec()
	num := x.Sub(math.LegacyOneDec())
	den := x.Add(math.LegacyOneDec())
	y := num.Quo(den)

	ySquared := y.Mul(y)
	term := y

	for i := 0; i < MaxIterations; i++ {
		// SAFETY: Consume Gas
		ctx.GasMeter().ConsumeGas(LmsrIterationGasCost, "lmsr_ln_iteration")

		n := math.LegacyNewDec(int64(2*i + 1))
		addToResult := term.Quo(n)
		result = result.Add(addToResult)

		term = term.Mul(ySquared)

		if addToResult.Abs().LT(Epsilon) {
			break
		}
	}

	return result.MulInt64(2), nil
}

// SettlementPriceYes returns p_yes = exp(qYes/b) / (exp(qYes/b) + exp(qNo/b)),
// the LMSR-implied probability of YES at the given pool state. Returns 1/2
// when both pools are zero (no information, fair price). Numerically stable:
// subtracts max(qYes, qNo)/b from both exponents so neither overflows.
//
// Used by CancelMarket and RESOLVED_INVALID resolution to snapshot a fair
// price at which holders can redeem their YES/NO shares (FUTARCHY-S2-1).
func SettlementPriceYes(ctx sdk.Context, b, qYes, qNo, maxExp math.LegacyDec) (math.LegacyDec, error) {
	if b.LTE(math.LegacyZeroDec()) {
		return math.LegacyZeroDec(), fmt.Errorf("SettlementPriceYes: b must be positive, got %s", b.String())
	}
	if qYes.IsZero() && qNo.IsZero() {
		// Symmetry: with no information, both outcomes are equally likely.
		return math.LegacyMustNewDecFromStr("0.5"), nil
	}

	x := qYes.Quo(b)
	y := qNo.Quo(b)
	x = ClampExponent(x, maxExp)
	y = ClampExponent(y, maxExp)

	max := x
	if y.GT(x) {
		max = y
	}
	expX := Exp(ctx, x.Sub(max))
	expY := Exp(ctx, y.Sub(max))
	sum := expX.Add(expY)
	if sum.IsZero() {
		return math.LegacyZeroDec(), fmt.Errorf("SettlementPriceYes: degenerate denominator")
	}
	return expX.Quo(sum), nil
}

// CreatorResidualResolved returns the LMSR remainder after a YES/NO winner
// claims 1 spark per winning share:
//
//	residual = b * ln(1 + exp((qLoser - qWinner) / b))
//
// This is the creator's correct WithdrawLiquidity payout for RESOLVED_YES and
// RESOLVED_NO markets (FUTARCHY-S2-1). Bounded between 0 (one-sided market)
// and InitialLiquidity = b*ln(2) (no trades or perfectly tied — but ties
// resolve INVALID, not YES/NO).
func CreatorResidualResolved(ctx sdk.Context, b, qWinner, qLoser, maxExp math.LegacyDec) (math.LegacyDec, error) {
	if b.LTE(math.LegacyZeroDec()) {
		return math.LegacyZeroDec(), fmt.Errorf("CreatorResidualResolved: b must be positive")
	}
	exponent := qLoser.Sub(qWinner).Quo(b)
	exponent = ClampExponent(exponent, maxExp)
	expTerm := Exp(ctx, exponent)
	onePlus := math.LegacyOneDec().Add(expTerm)
	lnTerm, err := Ln(ctx, onePlus)
	if err != nil {
		return math.LegacyZeroDec(), fmt.Errorf("CreatorResidualResolved: ln failed: %w", err)
	}
	return b.Mul(lnTerm), nil
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
