package types

import (
	"fmt"
)

const (
	// DefaultMaxTitleLength is the default maximum length for post titles
	DefaultMaxTitleLength uint64 = 200
	// DefaultMaxBodyLength is the default maximum length for post bodies
	DefaultMaxBodyLength uint64 = 10000
)

// NewParams creates a new Params instance.
func NewParams(maxTitleLength, maxBodyLength uint64) Params {
	return Params{
		MaxTitleLength: maxTitleLength,
		MaxBodyLength:  maxBodyLength,
	}
}

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	return NewParams(DefaultMaxTitleLength, DefaultMaxBodyLength)
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

	return nil
}
