package types

import (
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// DefaultMaxTitleLength is the default maximum length for post titles
	DefaultMaxTitleLength uint64 = 200
	// DefaultMaxBodyLength is the default maximum length for post bodies
	DefaultMaxBodyLength uint64 = 10000
	// DefaultFeeDenom is the default fee coin denomination
	DefaultFeeDenom = "uspark"
)

// DefaultCostPerByteAmount is the default per-byte storage cost (100 uspark/byte)
var DefaultCostPerByteAmount = math.NewInt(100)

// NewParams creates a new Params instance.
func NewParams(maxTitleLength, maxBodyLength uint64) Params {
	return Params{
		MaxTitleLength:   maxTitleLength,
		MaxBodyLength:    maxBodyLength,
		CostPerByte:      sdk.NewCoin(DefaultFeeDenom, DefaultCostPerByteAmount),
		CostPerByteExempt: false,
	}
}

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	return NewParams(DefaultMaxTitleLength, DefaultMaxBodyLength)
}

// DefaultBlogOperationalParams returns BlogOperationalParams with defaults
// matching the full Params defaults for cost_per_byte and cost_per_byte_exempt.
func DefaultBlogOperationalParams() BlogOperationalParams {
	return BlogOperationalParams{
		CostPerByte:       sdk.NewCoin(DefaultFeeDenom, DefaultCostPerByteAmount),
		CostPerByteExempt: false,
	}
}

// Validate validates the operational params.
func (op BlogOperationalParams) Validate() error {
	if !op.CostPerByte.Amount.IsNil() && op.CostPerByte.IsNegative() {
		return fmt.Errorf("cost_per_byte cannot be negative: %s", op.CostPerByte)
	}
	return nil
}

// ApplyOperationalParams copies CostPerByte and CostPerByteExempt from op,
// preserving MaxTitleLength and MaxBodyLength from the receiver.
func (p Params) ApplyOperationalParams(op BlogOperationalParams) Params {
	p.CostPerByte = op.CostPerByte
	p.CostPerByteExempt = op.CostPerByteExempt
	return p
}

// ExtractOperationalParams extracts the operational fields from the full params.
func (p Params) ExtractOperationalParams() BlogOperationalParams {
	return BlogOperationalParams{
		CostPerByte:       p.CostPerByte,
		CostPerByteExempt: p.CostPerByteExempt,
	}
}

// Validate validates the set of params.
func (p Params) Validate() error {
	if p.MaxTitleLength == 0 {
		return fmt.Errorf("max title length must be positive, got %d", p.MaxTitleLength)
	}

	if p.MaxBodyLength == 0 {
		return fmt.Errorf("max body length must be positive, got %d", p.MaxBodyLength)
	}

	// Sanity check: title should be shorter than body
	if p.MaxTitleLength > p.MaxBodyLength {
		return fmt.Errorf("max title length (%d) cannot exceed max body length (%d)",
			p.MaxTitleLength, p.MaxBodyLength)
	}

	if !p.CostPerByte.Amount.IsNil() && p.CostPerByte.IsNegative() {
		return fmt.Errorf("cost_per_byte cannot be negative: %s", p.CostPerByte)
	}

	return nil
}
