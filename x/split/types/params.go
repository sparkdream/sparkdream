package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

const DefaultCommonsCouncilFee string = "5000000uspark"
const DefaultCommonsCouncilAddress string = "commons_council_address"

var _ paramtypes.ParamSet = (*Params)(nil)

// NewParams creates a new Params instance.
func NewParams(commons_council_address string, commons_council_fee string) Params {
	return Params{CommonsCouncilAddress: commons_council_address, CommonsCouncilFee: commons_council_fee}
}

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	return NewParams(DefaultCommonsCouncilAddress, DefaultCommonsCouncilFee)
}

// ParamSetPairs implements the ParamSet interface and binds the parameters to the store.
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyCommonsCouncilAddress, &p.CommonsCouncilAddress, validateCommonsCouncilAddress),
		paramtypes.NewParamSetPair(KeyCommonsCouncilFee, &p.CommonsCouncilFee, validateCommonsCouncilFee),
	}
}

// Validate validates the set of params.
func (p Params) Validate() error {
	if err := validateCommonsCouncilAddress(p.CommonsCouncilAddress); err != nil {
		return err
	}
	if err := validateCommonsCouncilFee(p.CommonsCouncilFee); err != nil {
		return err
	}

	return nil
}

func validateCommonsCouncilAddress(i interface{}) error {
	v, ok := i.(string)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if v == "" {
		return nil
	}

	_, err := sdk.AccAddressFromBech32(v)
	if err != nil {
		return fmt.Errorf("invalid commons council address: %s", err)
	}

	return nil
}

func validateCommonsCouncilFee(i interface{}) error {
	v, ok := i.(string)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	// Allow empty string (means 0 fee / disabled)
	if v == "" {
		return nil
	}

	fee, err := sdk.ParseCoinsNormalized(v)
	if err != nil {
		return fmt.Errorf("invalid commons council fee format: %s", err)
	}

	// Ensure it is valid coins (non-negative)
	if !fee.IsValid() {
		return fmt.Errorf("invalid commons council fee coins: %s", fee)
	}

	return nil
}
