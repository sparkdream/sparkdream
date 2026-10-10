package artifact_test

import (
	"encoding/json"
	"math/rand"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/module"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/keeper"
	artifact "sparkdream/x/artifact/module"
	"sparkdream/x/artifact/types"
)

func TestGenerateGenesisState(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(artifact.AppModule{})
	simState := &module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
		Rand:      rand.New(rand.NewSource(1)),
		GenState:  make(map[string]json.RawMessage),
		Accounts:  simtypes.RandomAccounts(rand.New(rand.NewSource(1)), 3),
	}
	artifact.AppModule{}.GenerateGenesisState(simState)

	raw, ok := simState.GenState[types.ModuleName]
	require.True(t, ok)
	var genesis types.GenesisState
	require.NoError(t, encCfg.Codec.UnmarshalJSON(raw, &genesis))
	require.Equal(t, types.DefaultParams(), genesis.Params)
	require.NoError(t, genesis.Validate())
}

func TestWeightedOperations(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(artifact.AppModule{})
	am := artifact.NewAppModule(encCfg.Codec, keeper.Keeper{}, nil, nil)
	ops := am.WeightedOperations(module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
		TxConfig:  encCfg.TxConfig,
	})
	require.Len(t, ops, 11)
	for _, op := range ops {
		require.Positive(t, op.Weight())
	}
}

func TestProposalMsgs(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(artifact.AppModule{})
	am := artifact.NewAppModule(encCfg.Codec, keeper.Keeper{}, nil, nil)
	require.Empty(t, am.ProposalMsgs(module.SimulationState{AppParams: make(simtypes.AppParams), Cdc: encCfg.Codec}))
}
