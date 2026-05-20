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

// Recurring-spend params (MinRecurringPeriodSeconds /
// MaxRecurringDurationSeconds / MaxActiveRecurringSpendsPerGroup) live
// on x/session and apply uniformly to council-policy and user-account
// grants.

var _ paramtypes.ParamSet = (*Params)(nil)

// NewParams creates a new Params instance.
func NewParams(proposalFee string) Params {
	return Params{
		ProposalFee: proposalFee,
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
	}
}

// Validate validates the set of params.
func (p Params) Validate() error {
	return validateProposalFee(p.ProposalFee)
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
