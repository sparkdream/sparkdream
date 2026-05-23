package identity

import (
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/identity/types"
)

// GenerateGenesisState creates a deterministic simulation genesis. There is
// no randomization here: x/identity is genesis-only immutable and has no
// state space to fuzz. Sim genesis reuses the build-tag-selected
// DefaultChainIdentity so the resolved bond_denom / dream_denom line up
// with sdk.DefaultBondDenom (set in app.init from the same source) — the
// SDK simulator funds accounts and validators against sdk.DefaultBondDenom,
// and any other value here would put on-chain denom validators (e.g.
// x/session's allowed_denoms gate) out of sync with simulator-issued
// messages. allow_chain_id_mismatch=true keeps the chain-id consistency
// check from tripping on the simulator's "sparkdream-simapp" id.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	identityGenesis := types.GenesisState{
		Identity:             types.DefaultChainIdentity(),
		AllowChainIdMismatch: true,
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&identityGenesis)
}

// RegisterStoreDecoder is a no-op: identity holds two singleton items that
// the standard collections decoder already understands.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// WeightedOperations is empty: identity has no Msg surface.
func (am AppModule) WeightedOperations(_ module.SimulationState) []simtypes.WeightedOperation {
	return nil
}

// ProposalMsgs is empty: identity has no governance-routable messages.
func (am AppModule) ProposalMsgs(_ module.SimulationState) []simtypes.WeightedProposalMsg {
	return nil
}
