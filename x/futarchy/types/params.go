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
	if v > 10000 {
		return fmt.Errorf("trading_fee_bps must be <= 10000 (100%%)")
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

	return nil
}
