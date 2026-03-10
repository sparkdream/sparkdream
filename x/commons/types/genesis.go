package types

import "fmt"

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:               DefaultParams(),
		PolicyPermissionsMap: []PolicyPermissions{}, GroupMap: []Group{}}
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
	groupIndexMap := make(map[string]struct{})

	for _, elem := range gs.GroupMap {
		index := fmt.Sprint(elem.Index)
		if _, ok := groupIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for group")
		}
		groupIndexMap[index] = struct{}{}
	}

	return gs.Params.Validate()
}
