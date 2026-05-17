package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

var ForbiddenMessages = map[string]bool{
	// 1. RECURSION ATTACKS
	"/cosmos.authz.v1beta1.MsgExec":  true, // The "Sudo" command. Bypasses all your filters.
	"/cosmos.authz.v1beta1.MsgGrant": true, // Granting your power to an unchecked external wallet.

	// 2. ROOT KEY ATTACKS
	"/cosmos.group.v1.MsgCreateGroup":      true, // Only x/commons (the module) should create groups via your custom logic.
	"/cosmos.group.v1.MsgUpdateGroupAdmin": true, // Preventing a Coup (taking over the admin key).

	// 3. CONSENSUS ATTACKS
	"/cosmos.slashing.v1beta1.MsgUnjail":                 true, // A council shouldn't be able to unjail their own validators.
	"/cosmos.distribution.v1beta1.MsgSetWithdrawAddress": true, // Rerouting rewards silently.
}

const DefaultProposalFee string = "5000000uspark"

// Recurring-spend defaults. These align the feature with typical council
// pacing: minimum cadence of one day prevents pathologically tight
// schedules; one-year ceiling forces re-approval at the natural term
// boundary; 50 active schedules per council bounds state-bloat surface for
// a captured authority.
const (
	DefaultMinRecurringPeriodSeconds        int64  = 86_400     // 1 day
	DefaultMaxRecurringDurationSeconds      int64  = 31_536_000 // 365 days
	DefaultMaxActiveRecurringSpendsPerGroup uint32 = 50
)

// Key prefixes for the legacy params subspace. New recurring-spend params
// live on Params alongside ProposalFee; the keys exist so any consumer
// still using ParamSetPairs can observe them.
var (
	KeyMinRecurringPeriodSeconds        = []byte("MinRecurringPeriodSeconds")
	KeyMaxRecurringDurationSeconds      = []byte("MaxRecurringDurationSeconds")
	KeyMaxActiveRecurringSpendsPerGroup = []byte("MaxActiveRecurringSpendsPerGroup")
)

var _ paramtypes.ParamSet = (*Params)(nil)

// NewParams creates a new Params instance.
func NewParams(proposalFee string) Params {
	return Params{
		ProposalFee:                      proposalFee,
		MinRecurringPeriodSeconds:        DefaultMinRecurringPeriodSeconds,
		MaxRecurringDurationSeconds:      DefaultMaxRecurringDurationSeconds,
		MaxActiveRecurringSpendsPerGroup: DefaultMaxActiveRecurringSpendsPerGroup,
	}
}

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	return NewParams(DefaultProposalFee)
}

// ParamSetPairs implements the ParamSet interface and binds the parameters to the store.
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyProposalFee, &p.ProposalFee, validateProposalFee),
		paramtypes.NewParamSetPair(KeyMinRecurringPeriodSeconds, &p.MinRecurringPeriodSeconds, validateMinRecurringPeriodSeconds),
		paramtypes.NewParamSetPair(KeyMaxRecurringDurationSeconds, &p.MaxRecurringDurationSeconds, validateMaxRecurringDurationSeconds),
		paramtypes.NewParamSetPair(KeyMaxActiveRecurringSpendsPerGroup, &p.MaxActiveRecurringSpendsPerGroup, validateMaxActiveRecurringSpendsPerGroup),
	}
}

// Validate validates the set of params.
func (p Params) Validate() error {
	if err := validateProposalFee(p.ProposalFee); err != nil {
		return err
	}
	if err := validateMinRecurringPeriodSeconds(p.MinRecurringPeriodSeconds); err != nil {
		return err
	}
	if err := validateMaxRecurringDurationSeconds(p.MaxRecurringDurationSeconds); err != nil {
		return err
	}
	if err := validateMaxActiveRecurringSpendsPerGroup(p.MaxActiveRecurringSpendsPerGroup); err != nil {
		return err
	}
	// Cross-field: duration must be at least one period — otherwise no
	// schedule can ever fit.
	if p.MaxRecurringDurationSeconds > 0 && p.MinRecurringPeriodSeconds > 0 &&
		p.MaxRecurringDurationSeconds < p.MinRecurringPeriodSeconds {
		return fmt.Errorf("max_recurring_duration_seconds (%d) must be >= min_recurring_period_seconds (%d)",
			p.MaxRecurringDurationSeconds, p.MinRecurringPeriodSeconds)
	}
	return nil
}

func validateProposalFee(i interface{}) error {
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
		return fmt.Errorf("invalid commons proposal fee format: %s", err)
	}

	// Ensure it is valid coins (non-negative)
	if !fee.IsValid() {
		return fmt.Errorf("invalid commons proposal fee coins: %s", fee)
	}

	return nil
}

func validateMinRecurringPeriodSeconds(i interface{}) error {
	v, ok := i.(int64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v <= 0 {
		return fmt.Errorf("min_recurring_period_seconds must be > 0, got %d", v)
	}
	return nil
}

func validateMaxRecurringDurationSeconds(i interface{}) error {
	v, ok := i.(int64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v <= 0 {
		return fmt.Errorf("max_recurring_duration_seconds must be > 0, got %d", v)
	}
	return nil
}

func validateMaxActiveRecurringSpendsPerGroup(i interface{}) error {
	v, ok := i.(uint32)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v == 0 {
		return fmt.Errorf("max_active_recurring_spends_per_group must be > 0, got %d", v)
	}
	return nil
}
