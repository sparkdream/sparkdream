package types

import (
	"fmt"

	host "github.com/cosmos/ibc-go/v10/modules/core/24-host"
)

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params: DefaultParams(),
		PortId: PortID}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	if err := host.PortIdentifierValidator(gs.PortId); err != nil {
		return err
	}

	dayFundingIndex := make(map[uint64]struct{})
	for _, df := range gs.OperatorRewardDayFundingList {
		if _, ok := dayFundingIndex[df.Day]; ok {
			// A duplicate would be silently collapsed on import, under-reporting
			// the day's draw and handing back part of an allowance already spent.
			return fmt.Errorf("duplicated operator reward day funding for day %d", df.Day)
		}
		if df.AmountFunded.IsNil() || df.AmountFunded.IsNegative() {
			return fmt.Errorf("operator reward day funding for day %d must be non-negative", df.Day)
		}
		dayFundingIndex[df.Day] = struct{}{}
	}

	return gs.Params.Validate()
}
