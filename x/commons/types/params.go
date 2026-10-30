package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"

	identitytypes "sparkdream/x/identity/types"
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

	// 4. DELEGATION ESCAPES (the Spark Dream equivalents of authz MsgGrant /
	// MsgExec). A policy holding any of these could hand its signing power
	// or treasury to a single key and act without a proposal per action.
	// Council recurring spends go through the x/commons wrappers
	// (MsgScheduleRecurringSpend etc.), which keep the council's gates.
	"/sparkdream.session.v1.MsgCreateGrant":   true,
	"/sparkdream.session.v1.MsgCreateSession": true,
	"/sparkdream.session.v1.MsgExecSession":   true,
	"/sparkdream.shield.v1.MsgShieldedExec":   true, // anonymous execution as the policy
	"/sparkdream.guardian.v1.MsgExec":         true, // gov-authority guard: gov only
	"/sparkdream.vault.v1.MsgCreateVault":     true, // shared accounts: a policy is no signer
	"/sparkdream.vault.v1.MsgPropose":         true,
	"/sparkdream.vault.v1.MsgApprove":         true,
	"/sparkdream.vault.v1.MsgRevokeApproval":  true,
	"/sparkdream.vault.v1.MsgExecute":         true,
	"/sparkdream.vault.v1.MsgCancelProposal":  true,
	"/sparkdream.vault.v1.MsgUpdateVault":     true,
}

// DefaultProposalFee is 5 SPARK in the build's bond denom (the chain
// identity of the build, never a hardcoded "uspark": devnet, testnet and the
// test build each use their own denom). Networks pin the fee in config.yml.
var DefaultProposalFee = "5000000" + identitytypes.DefaultChainIdentity().BondDenom

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
