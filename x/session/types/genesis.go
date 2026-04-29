package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultGenesis returns the default genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:   DefaultParams(),
		Sessions: []Session{},
	}
}

// Validate performs basic genesis state validation returning an error upon any failure.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}

	// SESSION-7 fix: validate each imported session
	for i, session := range gs.Sessions {
		if _, err := sdk.AccAddressFromBech32(session.Granter); err != nil {
			return fmt.Errorf("session %d: invalid granter address %q: %w", i, session.Granter, err)
		}
		if _, err := sdk.AccAddressFromBech32(session.Grantee); err != nil {
			return fmt.Errorf("session %d: invalid grantee address %q: %w", i, session.Grantee, err)
		}
		if session.Expiration.IsZero() {
			return fmt.Errorf("session %d: expiration must be set", i)
		}
		if session.Granter == session.Grantee {
			return fmt.Errorf("session %d: granter cannot equal grantee", i)
		}
		if !session.SpendLimit.IsValid() || !session.SpendLimit.IsPositive() {
			return fmt.Errorf("session %d: spend_limit must be a valid positive coin", i)
		}
		if session.SpendLimit.Denom != "uspark" {
			return fmt.Errorf("session %d: spend_limit denom must be uspark, got %q", i, session.SpendLimit.Denom)
		}
		if !session.Spent.IsValid() {
			return fmt.Errorf("session %d: spent coin is invalid", i)
		}
		if session.Spent.Denom != session.SpendLimit.Denom {
			return fmt.Errorf("session %d: spent denom %q does not match spend_limit denom %q", i, session.Spent.Denom, session.SpendLimit.Denom)
		}
		if session.Spent.Amount.GT(session.SpendLimit.Amount) {
			return fmt.Errorf("session %d: spent (%s) exceeds spend_limit (%s)", i, session.Spent.Amount, session.SpendLimit.Amount)
		}
	}

	return nil
}
