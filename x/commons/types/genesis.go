package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

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

	// founding_members is either empty (use the build's compiled-in
	// founders) or a complete replacement, which must be able to bootstrap
	// governance on its own: valid unique addresses, display names for the
	// membership metadata, and exactly one founder (BootstrapGovernance
	// panics on a founderless member set, and a duplicate-founder spec is
	// almost certainly a misconfiguration).
	if len(gs.FoundingMembers) > 0 {
		founders := 0
		addrSeen := make(map[string]struct{}, len(gs.FoundingMembers))
		for _, m := range gs.FoundingMembers {
			if _, err := sdk.AccAddressFromBech32(m.Address); err != nil {
				return fmt.Errorf("invalid founding member address %q: %w", m.Address, err)
			}
			if _, ok := addrSeen[m.Address]; ok {
				return fmt.Errorf("duplicate founding member address %q", m.Address)
			}
			addrSeen[m.Address] = struct{}{}
			if m.DisplayName == "" {
				return fmt.Errorf("founding member %s has an empty display name", m.Address)
			}
			if m.Founder {
				founders++
			}
		}
		if founders != 1 {
			return fmt.Errorf("founding_members must mark exactly one founder, got %d", founders)
		}
	}

	return gs.Params.Validate()
}
