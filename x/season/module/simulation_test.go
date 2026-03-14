package season_test

import (
	"encoding/json"
	"math/rand"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/module"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	season "sparkdream/x/season/module"
	"sparkdream/x/season/types"
)

func TestGenerateGenesisState(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(season.AppModule{})
	simState := &module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
		Rand:      rand.New(rand.NewSource(1)),
		GenState:  make(map[string]json.RawMessage),
		Accounts:  simtypes.RandomAccounts(rand.New(rand.NewSource(1)), 3),
	}

	season.AppModule{}.GenerateGenesisState(simState)

	raw, ok := simState.GenState[types.ModuleName]
	require.True(t, ok, "genesis state should contain season module entry")

	var genesis types.GenesisState
	require.NoError(t, encCfg.Codec.UnmarshalJSON(raw, &genesis))

	require.Equal(t, types.DefaultParams(), genesis.Params)

	require.NotNil(t, genesis.Season, "genesis should contain an initial season")
	require.Equal(t, uint64(1), genesis.Season.Number)
	require.Equal(t, "Genesis Season", genesis.Season.Name)
	require.Equal(t, types.SeasonStatus_SEASON_STATUS_ACTIVE, genesis.Season.Status)
}

func TestWeightedOperations(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(season.AppModule{})
	am := season.NewAppModule(encCfg.Codec, keeper.Keeper{}, nil, nil)
	simState := module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
		TxConfig:  encCfg.TxConfig,
	}

	ops := am.WeightedOperations(simState)
	require.Len(t, ops, 40)
}

func TestProposalMsgs(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(season.AppModule{})
	am := season.NewAppModule(encCfg.Codec, keeper.Keeper{}, nil, nil)
	simState := module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
	}

	msgs := am.ProposalMsgs(simState)
	require.Len(t, msgs, 0)
}
