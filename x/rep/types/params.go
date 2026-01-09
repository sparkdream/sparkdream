package types

import (
	"fmt"

	"cosmossdk.io/math"
)

var (
	DefaultUnstakedDecayRate = math.LegacyNewDecWithPrec(1, 2) // 1%
	DefaultEpochBlocks       = int64(14400)                    // ~1 day
)

// NewParams creates a new Params instance.
func NewParams(
	unstakedDecayRate math.LegacyDec,
	epochBlocks int64,
) Params {
	return Params{
		UnstakedDecayRate: unstakedDecayRate,
		EpochBlocks:       epochBlocks,
	}
}

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	return NewParams(
		DefaultUnstakedDecayRate,
		DefaultEpochBlocks,
	)
}

// Validate validates the set of params.
func (p Params) Validate() error {
	if p.EpochBlocks <= 0 {
		return fmt.Errorf("epoch blocks must be positive: %d", p.EpochBlocks)
	}
	if p.UnstakedDecayRate.IsNegative() {
		return fmt.Errorf("decay rate cannot be negative: %s", p.UnstakedDecayRate)
	}
	if p.UnstakedDecayRate.GT(math.LegacyOneDec()) {
		return fmt.Errorf("decay rate cannot be greater than 1: %s", p.UnstakedDecayRate)
	}

	return nil
}
