package types

import (
	fmt "fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

const DefaultCommonsCouncilAddress string = "commons_council_address"

var _ paramtypes.ParamSet = (*Params)(nil)

// NewParams creates a new Params instance.
func NewParams(commons_council_address string) Params {
	return Params{CommonsCouncilAddress: commons_council_address}
}

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	return NewParams(DefaultCommonsCouncilAddress)
}

// ParamSetPairs implements the ParamSet interface and binds the parameters to the store.
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyCommonsCouncilAddress, &p.CommonsCouncilAddress, validateCommonsCouncilAddress),
	}
}

// Validate validates the set of params.
func (p Params) Validate() error {
	if err := validateCommonsCouncilAddress(p.CommonsCouncilAddress); err != nil {
		return err
	}

	return nil
}

// validateCommonsCouncilAddress validates the CommonsCouncilAddress param
// Note: The argument must be an interface{} to satisfy the ParamSetPairs validator signature
func validateCommonsCouncilAddress(i interface{}) error {
	v, ok := i.(string)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	// 1. Allow empty address (optional, if you want to allow disabling the council)
	if v == "" {
		return nil
	}

	// 2. Validate Bech32
	// This ensures it's a valid Cosmos address
	_, err := sdk.AccAddressFromBech32(v)
	if err != nil {
		return fmt.Errorf("invalid commons council address: %s", err)
	}

	return nil
}
