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
	}

	return nil
}
