package types

import "fmt"

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:               DefaultParams(),
		PolicyPermissionsMap: []PolicyPermissions{}}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	policyPermissionsIndexMap := make(map[string]struct{})

	for _, elem := range gs.PolicyPermissionsMap {
		index := fmt.Sprint(elem.PolicyAddress)
		if _, ok := policyPermissionsIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for policyPermissions")
		}
		policyPermissionsIndexMap[index] = struct{}{}
	}

	return gs.Params.Validate()
}
