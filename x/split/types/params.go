package types

// NewParams creates a new Params instance.
func NewParams() Params {
	return Params{}
}

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	return NewParams()
}

// Validate validates the set of params.
// TODO: Add validation for split-specific parameters once they are defined
// (e.g., minimum share weight, maximum number of shares, distribution frequency).
func (p Params) Validate() error {
	return nil
}
