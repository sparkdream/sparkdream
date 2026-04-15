package app

import (
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
)

// gnovmSimOverride disables gnovm's simulation operations because the upstream
// SimulateMsgRun returns a non-nil error on failure (instead of nil), which the
// simulation framework treats as fatal and aborts the entire run.
type gnovmSimOverride struct{}

func (gnovmSimOverride) GenerateGenesisState(_ *module.SimulationState)       {}
func (gnovmSimOverride) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}
func (gnovmSimOverride) WeightedOperations(_ module.SimulationState) []simtypes.WeightedOperation {
	return nil
}
