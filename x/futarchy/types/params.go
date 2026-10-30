package types

import (
	"fmt"

	"cosmossdk.io/math"
)

// NewParams creates a new Params instance.
func NewParams(
	minLiquidity math.Int,
	maxDuration int64,
	defaultMinTick math.Int,
	maxRedemptionDelay int64,
	tradingFeeBps uint64,
	maxLmsrExponent string,
) Params {
	return Params{
		MinLiquidity:       minLiquidity,
		MaxDuration:        maxDuration,
		DefaultMinTick:     defaultMinTick,
		MaxRedemptionDelay: maxRedemptionDelay,
		TradingFeeBps:      tradingFeeBps,
		MaxLmsrExponent:    maxLmsrExponent,
	}
}

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	return NewParams(
		DefaultMinLiquidity,
		DefaultMaxDuration,
		DefaultMinTick,
		DefaultMaxRedemptionDelay,
		DefaultTradingFeeBps,
		DefaultMaxLmsrExponent,
	)
}

// Validate validates the set of params.
func (p Params) Validate() error {
	if err := validateMinLiquidity(p.MinLiquidity); err != nil {
		return err
	}
	if err := validateMaxDuration(p.MaxDuration); err != nil {
		return err
	}
	if err := validateDefaultMinTick(p.DefaultMinTick); err != nil {
		return err
	}
	if err := validateMaxRedemptionDelay(p.MaxRedemptionDelay); err != nil {
		return err
	}
	if err := validateTradingFeeBps(p.TradingFeeBps); err != nil {
		return err
	}
	if err := validateMaxLmsrExponent(p.MaxLmsrExponent); err != nil {
		return err
	}

	return nil
}

// DefaultFutarchyOperationalParams returns FutarchyOperationalParams with defaults
// matching the existing Params defaults for the 3 operational fields.
func DefaultFutarchyOperationalParams() FutarchyOperationalParams {
	return FutarchyOperationalParams{
		TradingFeeBps:      DefaultTradingFeeBps,
		MaxDuration:        DefaultMaxDuration,
		MaxRedemptionDelay: DefaultMaxRedemptionDelay,
	}
}

// Validate validates the operational params.
func (op FutarchyOperationalParams) Validate() error {
	if err := validateTradingFeeBps(op.TradingFeeBps); err != nil {
		return err
	}
	if err := validateMaxDuration(op.MaxDuration); err != nil {
		return err
	}
	if err := validateMaxRedemptionDelay(op.MaxRedemptionDelay); err != nil {
		return err
	}
	return nil
}

// ApplyOperationalParams copies the 3 operational fields from op,
// preserving MinLiquidity, DefaultMinTick, MaxLmsrExponent.
func (p Params) ApplyOperationalParams(op FutarchyOperationalParams) Params {
	p.TradingFeeBps = op.TradingFeeBps
	p.MaxDuration = op.MaxDuration
	p.MaxRedemptionDelay = op.MaxRedemptionDelay
	return p
}

// ExtractOperationalParams extracts the operational fields from the full params.
func (p Params) ExtractOperationalParams() FutarchyOperationalParams {
	return FutarchyOperationalParams{
		TradingFeeBps:      p.TradingFeeBps,
		MaxDuration:        p.MaxDuration,
		MaxRedemptionDelay: p.MaxRedemptionDelay,
	}
}

// Compiled hard bounds. Governance and the operations committee can move the
// params only inside these limits; changing a bound needs a chain upgrade.
var (
	// MaxTradingFeeBps caps trading_fee_bps at 5% (FUT-10). A higher fee
	// would let a single operational-params update stop trading outright.
	MaxTradingFeeBps uint64 = 500

	// MaxLmsrExponentCeiling caps max_lmsr_exponent (FUT-04). The param
	// bounds |q_yes - q_no| / b, i.e. how close to 0 or 1 a market price may
	// be pushed; e^-30 ~ 9e-14 is already below any meaningful price.
	MaxLmsrExponentCeiling = math.LegacyNewDec(30)
)

// Validation Functions ------------------------------------------------------

func validateMinLiquidity(i interface{}) error {
	v, ok := i.(math.Int)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v.IsNil() || v.LTE(math.ZeroInt()) {
		return fmt.Errorf("min_liquidity must be positive")
	}
	return nil
}

func validateMaxDuration(i interface{}) error {
	v, ok := i.(int64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v <= 0 {
		return fmt.Errorf("max_duration must be positive")
	}
	return nil
}

func validateDefaultMinTick(i interface{}) error {
	v, ok := i.(math.Int)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v.IsNil() || v.LT(math.ZeroInt()) {
		return fmt.Errorf("default_min_tick must be non-negative")
	}
	return nil
}

func validateMaxRedemptionDelay(i interface{}) error {
	v, ok := i.(int64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v < 0 {
		return fmt.Errorf("max_redemption_delay must be non-negative")
	}
	return nil
}

func validateTradingFeeBps(i interface{}) error {
	v, ok := i.(uint64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v > MaxTradingFeeBps {
		return fmt.Errorf("trading_fee_bps must be <= %d (compiled hard ceiling)", MaxTradingFeeBps)
	}
	return nil
}

func validateMaxLmsrExponent(i interface{}) error {
	v, ok := i.(string)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v == "" {
		return fmt.Errorf("max_lmsr_exponent cannot be empty")
	}

	maxExp, err := math.LegacyNewDecFromStr(v)
	if err != nil {
		return fmt.Errorf("invalid max_lmsr_exponent: %w", err)
	}

	if maxExp.LTE(math.LegacyZeroDec()) {
		return fmt.Errorf("max_lmsr_exponent must be positive")
	}
	if maxExp.GT(MaxLmsrExponentCeiling) {
		return fmt.Errorf("max_lmsr_exponent must be <= %s (compiled hard ceiling)", MaxLmsrExponentCeiling.String())
	}

	return nil
}
