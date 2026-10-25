package types

import "fmt"

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:   DefaultParams(),
		VaultMap: []Vault{}, VaultProposalMap: []VaultProposal{}}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	vaultIndexMap := make(map[string]struct{})

	for _, elem := range gs.VaultMap {
		index := fmt.Sprint(elem.VaultId)
		if _, ok := vaultIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for vault")
		}
		vaultIndexMap[index] = struct{}{}
	}
	vaultProposalIndexMap := make(map[string]struct{})

	for _, elem := range gs.VaultProposalMap {
		index := fmt.Sprint(elem.ProposalId)
		if _, ok := vaultProposalIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for vaultProposal")
		}
		vaultProposalIndexMap[index] = struct{}{}
	}

	return gs.Params.Validate()
}
