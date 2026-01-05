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

	// Default max exponent for safety (can be overridden by params)
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

// CalculateLMSRCost now accepts ctx and validates inputs
func CalculateLMSRCost(ctx sdk.Context, b math.LegacyDec, qYes, qNo math.LegacyDec) math.LegacyDec {
	// Validate b is positive
	if b.LTE(math.LegacyZeroDec()) {
		panic(fmt.Sprintf("CalculateLMSRCost: b must be positive, got %s", b.String()))
	}

	x := qYes.Quo(b)
	y := qNo.Quo(b)

	// Clamp exponents for numerical stability
	x = ClampExponent(x, DefaultMaxExponent)
	y = ClampExponent(y, DefaultMaxExponent)

	max := x
	if y.GT(x) {
		max = y
	}

	// Pass ctx to Exp
	term1 := Exp(ctx, x.Sub(max))
	term2 := Exp(ctx, y.Sub(max))

	sum := term1.Add(term2)

	// Pass ctx to Ln
	lnSum := Ln(ctx, sum)
	result := b.Mul(max.Add(lnSum))

	return result
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

// Ln consumes gas per loop
func Ln(ctx sdk.Context, x math.LegacyDec) math.LegacyDec {
	if x.LTE(math.LegacyZeroDec()) {
		panic("Ln undefined for <= 0")
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

	return result.MulInt64(2)
}
