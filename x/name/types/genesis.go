package types

import "fmt"

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:      DefaultParams(),
		NameRecords: []NameRecord{},
		OwnerInfos:  []OwnerInfo{},
		DisputeMap:  []Dispute{},
	}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	// Validate NameRecords
	nameRecordMap := make(map[string]struct{})
	for _, elem := range gs.NameRecords {
		name := fmt.Sprint(elem.Name)
		if _, ok := nameRecordMap[name]; ok {
			return fmt.Errorf("duplicated name for nameRecord")
		}
		nameRecordMap[name] = struct{}{}
	}

	// Validate OwnerInfos
	ownerInfoMap := make(map[string]struct{})
	for _, elem := range gs.OwnerInfos {
		address := fmt.Sprint(elem.Address)
		if _, ok := ownerInfoMap[address]; ok {
			return fmt.Errorf("duplicated address for ownerInfo")
		}
		ownerInfoMap[address] = struct{}{}
	}

	// Validate Disputes
	disputeNameMap := make(map[string]struct{})
	for _, elem := range gs.DisputeMap {
		name := fmt.Sprint(elem.Name)
		if _, ok := disputeNameMap[name]; ok {
			return fmt.Errorf("duplicated name for dispute")
		}
		disputeNameMap[name] = struct{}{}
	}

	return gs.Params.Validate()
}
