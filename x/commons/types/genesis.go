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
	if err := gs.validateGroupReferences(groupIndexMap); err != nil {
		return err
	}
	if err := gs.validateAnonEntries(); err != nil {
		return err
	}
	if err := gs.validatePolls(); err != nil {
		return err
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

// validateGroupReferences checks what InitGenesis derives from group_map:
// the group name is the collection key (an empty index would collapse every
// group onto ""), and the policy and veto addresses become PolicyToName /
// VetoPolicies entries, so no two groups may claim the same address. Member
// rows must belong to an imported group. A genesis without groups is a
// launch genesis: InitGenesis bootstraps the councils, so there is nothing
// to cross-check.
func (gs GenesisState) validateGroupReferences(groups map[string]struct{}) error {
	if len(gs.GroupMap) == 0 {
		return nil
	}
	policyOwner := make(map[string]string, 2*len(gs.GroupMap))
	claim := func(addr, group string) error {
		if prev, ok := policyOwner[addr]; ok {
			return fmt.Errorf("policy address %s is claimed by groups %q and %q", addr, prev, group)
		}
		policyOwner[addr] = group
		return nil
	}
	for _, g := range gs.GroupMap {
		if g.Index == "" {
			return fmt.Errorf("group with policy %q has an empty index", g.PolicyAddress)
		}
		if g.PolicyAddress != "" {
			if err := claim(g.PolicyAddress, g.Index); err != nil {
				return err
			}
		}
		if g.VetoPolicyAddress != "" {
			if err := claim(g.VetoPolicyAddress, g.Index); err != nil {
				return err
			}
		}
	}
	for _, cm := range gs.CouncilMembers {
		if _, ok := groups[cm.CouncilName]; !ok {
			return fmt.Errorf("council_members references unknown group %q", cm.CouncilName)
		}
	}
	return nil
}

// validateAnonEntries rejects duplicate anonymous tallies and counters (the
// second would silently overwrite the first at import).
func (gs GenesisState) validateAnonEntries() error {
	tallies := make(map[uint64]struct{}, len(gs.AnonVoteTallies))
	for _, e := range gs.AnonVoteTallies {
		if _, ok := tallies[e.ProposalId]; ok {
			return fmt.Errorf("duplicated anon vote tally for proposal %d", e.ProposalId)
		}
		tallies[e.ProposalId] = struct{}{}
	}
	type dayKey struct {
		council string
		day     int64
	}
	counts := make(map[dayKey]struct{}, len(gs.AnonProposalCounts))
	for _, e := range gs.AnonProposalCounts {
		if e.CouncilName == "" {
			return fmt.Errorf("anon proposal count with an empty council name")
		}
		key := dayKey{e.CouncilName, e.UtcDay}
		if _, ok := counts[key]; ok {
			return fmt.Errorf("duplicated anon proposal count for %s day %d", e.CouncilName, e.UtcDay)
		}
		counts[key] = struct{}{}
	}
	return nil
}
