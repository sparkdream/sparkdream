package types

import "fmt"

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:    DefaultParams(),
		MarketMap: []Market{}}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	marketIndexMap := make(map[string]struct{})

	for _, elem := range gs.MarketMap {
		index := fmt.Sprint(elem.Index)
		if _, ok := marketIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for market")
		}
		marketIndexMap[index] = struct{}{}
	}

	return gs.Params.Validate()
}
