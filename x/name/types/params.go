package types

import (
	"fmt"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

var _ paramtypes.ParamSet = (*Params)(nil)

// ParamKeyTable the param key table for launch module
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

// NewParams creates a new Params instance
func NewParams(
	blockedNames []string,
	minNameLength uint64,
	maxNameLength uint64,
	maxNamesPerAddress uint64,
	expirationDuration time.Duration,
	registrationFee sdk.Coin,
	disputeStakeDream math.Int,
	disputeTimeoutBlocks uint64,
	contestStakeDream math.Int,
) Params {
	return Params{
		BlockedNames:         blockedNames,
		MinNameLength:        minNameLength,
		MaxNameLength:        maxNameLength,
		MaxNamesPerAddress:   maxNamesPerAddress,
		ExpirationDuration:   expirationDuration,
		RegistrationFee:      registrationFee,
		DisputeStakeDream:    disputeStakeDream,
		DisputeTimeoutBlocks: disputeTimeoutBlocks,
		ContestStakeDream:    contestStakeDream,
	}
}

// DefaultParams returns a default set of parameters
func DefaultParams() Params {
	return NewParams(
		DefaultBlockedNames,
		DefaultMinNameLength,
		DefaultMaxNameLength,
		DefaultMaxNamesPerAddress,
		DefaultExpirationDuration,
		DefaultRegistrationFee,
		DefaultDisputeStakeDream,
		DefaultDisputeTimeoutBlocks,
		DefaultContestStakeDream,
	)
}

// ParamSetPairs get the params.ParamSet
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyBlockedNames, &p.BlockedNames, validateBlockedNames),
		paramtypes.NewParamSetPair(KeyMinNameLength, &p.MinNameLength, validateMinNameLength),
		paramtypes.NewParamSetPair(KeyMaxNameLength, &p.MaxNameLength, validateMaxNameLength),
		paramtypes.NewParamSetPair(KeyMaxNamesPerAddress, &p.MaxNamesPerAddress, validateMaxNamesPerAddress),
		paramtypes.NewParamSetPair(KeyExpirationDuration, &p.ExpirationDuration, validateExpirationDuration),
		paramtypes.NewParamSetPair(KeyRegistrationFee, &p.RegistrationFee, validateRegistrationFee),
	}
}

// Validate validates the set of params
func (p Params) Validate() error {
	if err := validateBlockedNames(p.BlockedNames); err != nil {
		return err
	}
	if err := validateMinNameLength(p.MinNameLength); err != nil {
		return err
	}
	if err := validateMaxNameLength(p.MaxNameLength); err != nil {
		return err
	}
	if err := validateMaxNamesPerAddress(p.MaxNamesPerAddress); err != nil {
		return err
	}
	if err := validateExpirationDuration(p.ExpirationDuration); err != nil {
		return err
	}
	if err := validateRegistrationFee(p.RegistrationFee); err != nil {
		return err
	}
	if p.DisputeStakeDream.IsNegative() {
		return fmt.Errorf("dispute stake must be non-negative")
	}
	if p.ContestStakeDream.IsNegative() {
		return fmt.Errorf("contest stake must be non-negative")
	}

	return nil
}

// Validation Functions ------------------------------------------------------

func validateBlockedNames(i interface{}) error {
	v, ok := i.([]string)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	for _, name := range v {
		if len(name) == 0 {
			return fmt.Errorf("blocked name cannot be empty")
		}
	}
	return nil
}

func validateMinNameLength(i interface{}) error {
	v, ok := i.(uint64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v == 0 {
		return fmt.Errorf("min name length must be positive")
	}
	return nil
}

func validateMaxNameLength(i interface{}) error {
	v, ok := i.(uint64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v == 0 {
		return fmt.Errorf("max name length must be positive")
	}
	return nil
}

func validateMaxNamesPerAddress(i interface{}) error {
	v, ok := i.(uint64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v == 0 {
		return fmt.Errorf("max names per address must be positive")
	}
	return nil
}

func validateExpirationDuration(i interface{}) error {
	v, ok := i.(time.Duration)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v <= 0 {
		return fmt.Errorf("expiration duration must be positive")
	}
	return nil
}

func validateRegistrationFee(i interface{}) error {
	v, ok := i.(sdk.Coin)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if !v.IsValid() {
		return fmt.Errorf("invalid registration fee coin: %s", v)
	}
	return nil
}
