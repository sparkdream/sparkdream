package types

import "fmt"

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:        DefaultParams(),
		NameRecords:   []NameRecord{},
		OwnerInfos:    []OwnerInfo{},
		DisputeMap:    []Dispute{},
		DisputeStakes: []DisputeStake{},
		ContestStakes: []ContestStake{},
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

	// Validate stake records: unique challenge ids, non-negative amounts.
	disputeStakeMap := make(map[string]struct{})
	for _, elem := range gs.DisputeStakes {
		if _, ok := disputeStakeMap[elem.ChallengeId]; ok {
			return fmt.Errorf("duplicated challenge_id for disputeStake")
		}
		if elem.Amount.IsNil() || elem.Amount.IsNegative() {
			return fmt.Errorf("invalid amount for disputeStake %s", elem.ChallengeId)
		}
		disputeStakeMap[elem.ChallengeId] = struct{}{}
	}
	contestStakeMap := make(map[string]struct{})
	for _, elem := range gs.ContestStakes {
		if _, ok := contestStakeMap[elem.ChallengeId]; ok {
			return fmt.Errorf("duplicated challenge_id for contestStake")
		}
		if elem.Amount.IsNil() || elem.Amount.IsNegative() {
			return fmt.Errorf("invalid amount for contestStake %s", elem.ChallengeId)
		}
		contestStakeMap[elem.ChallengeId] = struct{}{}
	}

	return gs.Params.Validate()
}
