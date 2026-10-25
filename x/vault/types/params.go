package types

import (
	"fmt"

	"cosmossdk.io/math"
)

const DefaultCreationEnabled bool = true

// DefaultCreationFeeAmount is 10 SPARK in micro-units, the same spam pricing
// as tag creation.
var DefaultCreationFeeAmount = math.NewInt(10_000_000)

// NewParams creates a new Params instance.
func NewParams(creationEnabled bool, creationFeeAmount math.Int) Params {
	return Params{CreationEnabled: creationEnabled, CreationFeeAmount: creationFeeAmount}
}

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	return NewParams(DefaultCreationEnabled, DefaultCreationFeeAmount)
}

// Validate validates the set of params.
func (p Params) Validate() error {
	if p.CreationFeeAmount.IsNil() || p.CreationFeeAmount.IsNegative() {
		return fmt.Errorf("creation_fee_amount must be a non-negative integer")
	}
	return nil
}
