package types

import "fmt"

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
		shareIndexMap[index] = struct{}{}
	}

	return gs.Params.Validate()
}
