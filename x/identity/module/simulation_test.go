package identity_test

import (
	"encoding/json"
	"math/rand"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/module"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/stretchr/testify/require"

	identity "sparkdream/x/identity/module"
	"sparkdream/x/identity/types"
)

func TestGenerateGenesisState(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(identity.AppModule{})
	simState := &module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
		Rand:      rand.New(rand.NewSource(1)),
		GenState:  make(map[string]json.RawMessage),
		Accounts:  simtypes.RandomAccounts(rand.New(rand.NewSource(1)), 3),
	}
	identity.AppModule{}.GenerateGenesisState(simState)

	raw, ok := simState.GenState[types.ModuleName]
	require.True(t, ok, "expected GenState to contain entry for module %q", types.ModuleName)

	var gs types.GenesisState
	require.NoError(t, encCfg.Codec.UnmarshalJSON(raw, &gs))
	require.NoError(t, gs.Validate(), "simulation genesis must validate")
	require.NotEmpty(t, gs.Identity.BondDenom)
	require.NotEmpty(t, gs.Identity.DreamDenom)
	require.NotEqual(t, gs.Identity.BondDenom, gs.Identity.DreamDenom)
	require.True(t, gs.AllowChainIdMismatch, "sim genesis sets allow_chain_id_mismatch so any sim chain_id is accepted")
}

func TestSimulationEmptySurfaces(t *testing.T) {
	// Identity has no Msg surface and no governance proposals; the slices
	// from WeightedOperations and ProposalMsgs must be empty/nil.
	require.Nil(t, identity.AppModule{}.WeightedOperations(module.SimulationState{}))
	require.Nil(t, identity.AppModule{}.ProposalMsgs(module.SimulationState{}))
}
