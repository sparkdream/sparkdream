package types

import (
	"fmt"
	"time"

	"cosmossdk.io/math"
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
	registrationFeeAmount math.Int,
	disputeStakeDream math.Int,
	disputeTimeoutBlocks uint64,
	contestStakeDream math.Int,
) Params {
	return Params{
		BlockedNames:          blockedNames,
		MinNameLength:         minNameLength,
		MaxNameLength:         maxNameLength,
		MaxNamesPerAddress:    maxNamesPerAddress,
		ExpirationDuration:    expirationDuration,
		RegistrationFeeAmount: registrationFeeAmount,
		DisputeStakeDream:     disputeStakeDream,
		DisputeTimeoutBlocks:  disputeTimeoutBlocks,
		ContestStakeDream:     contestStakeDream,
	}
}

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	return NewParams(
		DefaultBlockedNames,
		DefaultMinNameLength,
		DefaultMaxNameLength,
		DefaultMaxNamesPerAddress,
		DefaultExpirationDuration,
		DefaultRegistrationFeeAmount,
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
		paramtypes.NewParamSetPair(KeyRegistrationFee, &p.RegistrationFeeAmount, validateRegistrationFee),
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
	if err := validateRegistrationFee(p.RegistrationFeeAmount); err != nil {
		return err
	}
	if p.DisputeStakeDream.IsNegative() {
		return fmt.Errorf("dispute stake must be non-negative")
	}
	if !p.DisputeStakeDream.IsUint64() {
		return fmt.Errorf("dispute stake must fit in uint64")
	}
	if p.ContestStakeDream.IsNegative() {
		return fmt.Errorf("contest stake must be non-negative")
	}
	if !p.ContestStakeDream.IsUint64() {
		return fmt.Errorf("contest stake must fit in uint64")
	}
	if p.DisputeTimeoutBlocks == 0 {
		return fmt.Errorf("dispute timeout blocks must be positive")
	}
	if p.MinNameLength > p.MaxNameLength {
		return fmt.Errorf("min name length %d exceeds max name length %d", p.MinNameLength, p.MaxNameLength)
	}

	return nil
}

// DefaultNameOperationalParams returns NameOperationalParams with defaults
// matching the existing Params defaults for the 5 operational fields.
func DefaultNameOperationalParams() NameOperationalParams {
	return NameOperationalParams{
		ExpirationDuration:    DefaultExpirationDuration,
		RegistrationFeeAmount: DefaultRegistrationFeeAmount,
		DisputeStakeDream:     DefaultDisputeStakeDream,
		DisputeTimeoutBlocks:  DefaultDisputeTimeoutBlocks,
		ContestStakeDream:     DefaultContestStakeDream,
	}
}

// Compiled hard bounds on the operational params. The Commons Operations
// Committee may tune these values (MsgUpdateOperationalParams) only inside
// the bounds; a value that would make names instantly scavengeable or
// disputes instantly upheld (NAM-01) is refused. x/gov (MsgUpdateParams) is
// not bound by them.
var (
	// MinOpExpirationDuration: an owner must be inactive this long before
	// their names can be scavenged.
	MinOpExpirationDuration = 180 * 24 * time.Hour
	// MaxOpExpirationDuration caps it so scavenging stays possible.
	MaxOpExpirationDuration = 10 * 365 * 24 * time.Hour
	// MinOpDisputeTimeoutBlocks: the owner's contest window (~3 days at 6s).
	MinOpDisputeTimeoutBlocks uint64 = 43_200
	// MaxOpDisputeTimeoutBlocks: ~60 days at 6s.
	MaxOpDisputeTimeoutBlocks uint64 = 864_000
	// MinOpStakeDream: 1 DREAM floor on the dispute and contest stakes, so
	// filing a dispute always costs something.
	MinOpStakeDream = math.NewInt(1_000_000)
	// MaxOpContestStakeDream: 1,000 DREAM ceiling on the contest stake, so
	// owners are not priced out of defending their names.
	MaxOpContestStakeDream = math.NewInt(1_000_000_000)
	// MaxOpRegistrationFeeAmount: 1,000 SPARK ceiling on the registration
	// fee.
	MaxOpRegistrationFeeAmount = math.NewInt(1_000_000_000)
)

// Validate validates the operational params against the compiled bounds.
func (op NameOperationalParams) Validate() error {
	if op.ExpirationDuration <= 0 {
		return fmt.Errorf("expiration duration must be positive")
	}
	if op.ExpirationDuration < MinOpExpirationDuration || op.ExpirationDuration > MaxOpExpirationDuration {
		return fmt.Errorf("expiration duration %s outside [%s, %s]", op.ExpirationDuration, MinOpExpirationDuration, MaxOpExpirationDuration)
	}
	if err := validateRegistrationFee(op.RegistrationFeeAmount); err != nil {
		return err
	}
	if op.RegistrationFeeAmount.GT(MaxOpRegistrationFeeAmount) {
		return fmt.Errorf("registration fee %s exceeds %s", op.RegistrationFeeAmount, MaxOpRegistrationFeeAmount)
	}
	if op.DisputeStakeDream.IsNil() || op.DisputeStakeDream.IsNegative() {
		return fmt.Errorf("dispute stake must be non-negative")
	}
	if op.ContestStakeDream.IsNil() || op.ContestStakeDream.IsNegative() {
		return fmt.Errorf("contest stake must be non-negative")
	}
	if op.DisputeStakeDream.LT(MinOpStakeDream) {
		return fmt.Errorf("dispute stake %s below the %s floor", op.DisputeStakeDream, MinOpStakeDream)
	}
	if op.ContestStakeDream.LT(MinOpStakeDream) || op.ContestStakeDream.GT(MaxOpContestStakeDream) {
		return fmt.Errorf("contest stake %s outside [%s, %s]", op.ContestStakeDream, MinOpStakeDream, MaxOpContestStakeDream)
	}
	if op.DisputeTimeoutBlocks == 0 {
		return fmt.Errorf("dispute timeout blocks must be positive")
	}
	if op.DisputeTimeoutBlocks < MinOpDisputeTimeoutBlocks || op.DisputeTimeoutBlocks > MaxOpDisputeTimeoutBlocks {
		return fmt.Errorf("dispute timeout %d blocks outside [%d, %d]", op.DisputeTimeoutBlocks, MinOpDisputeTimeoutBlocks, MaxOpDisputeTimeoutBlocks)
	}
	return nil
}

// ApplyOperationalParams copies the 5 operational fields from op,
// preserving BlockedNames, MinNameLength, MaxNameLength, MaxNamesPerAddress.
func (p Params) ApplyOperationalParams(op NameOperationalParams) Params {
	p.ExpirationDuration = op.ExpirationDuration
	p.RegistrationFeeAmount = op.RegistrationFeeAmount
	p.DisputeStakeDream = op.DisputeStakeDream
	p.DisputeTimeoutBlocks = op.DisputeTimeoutBlocks
	p.ContestStakeDream = op.ContestStakeDream
	return p
}

// ExtractOperationalParams extracts the operational fields from the full params.
func (p Params) ExtractOperationalParams() NameOperationalParams {
	return NameOperationalParams{
		ExpirationDuration:    p.ExpirationDuration,
		RegistrationFeeAmount: p.RegistrationFeeAmount,
		DisputeStakeDream:     p.DisputeStakeDream,
		DisputeTimeoutBlocks:  p.DisputeTimeoutBlocks,
		ContestStakeDream:     p.ContestStakeDream,
	}
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
	v, ok := i.(math.Int)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v.IsNil() {
		return fmt.Errorf("registration fee amount must not be nil")
	}
	if v.IsNegative() {
		return fmt.Errorf("registration fee amount must not be negative: %s", v)
	}
	return nil
}
