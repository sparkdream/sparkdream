package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:   DefaultParams(),
		ShareMap: []Share{}}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	shareIndexMap := make(map[string]struct{})

	for _, elem := range gs.ShareMap {
		index := fmt.Sprint(elem.Address)
		if _, ok := shareIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for share")
		}
		// Validate that each share address is a valid bech32 address
		if _, err := sdk.AccAddressFromBech32(elem.Address); err != nil {
			return fmt.Errorf("invalid share address %q: %w", elem.Address, err)
		}
		shareIndexMap[index] = struct{}{}
	}

	return gs.Params.Validate()
}
