package vote_test

import (
	"encoding/json"
	"math/rand"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/module"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/stretchr/testify/require"

	vote "sparkdream/x/vote/module"
	"sparkdream/x/vote/keeper"
	"sparkdream/x/vote/types"
)

func TestGenerateGenesisState(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(vote.AppModule{})
	simState := &module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
		Rand:      rand.New(rand.NewSource(1)),
		GenState:  make(map[string]json.RawMessage),
		Accounts:  simtypes.RandomAccounts(rand.New(rand.NewSource(1)), 3),
	}

	vote.AppModule{}.GenerateGenesisState(simState)

	raw, ok := simState.GenState[types.ModuleName]
	require.True(t, ok)
	require.NotEmpty(t, raw)

	var genesis types.GenesisState
	require.NoError(t, encCfg.Codec.UnmarshalJSON(raw, &genesis))

	// Verify key known fields rather than full Equal to avoid nil vs empty
	// slice differences from proto JSON round-trip.
	require.Equal(t, types.DefaultParams().TreeDepth, genesis.Params.TreeDepth)
	require.Equal(t, types.DefaultParams().MinVotingPeriodEpochs, genesis.Params.MinVotingPeriodEpochs)
	require.Equal(t, types.DefaultParams().MaxVotingPeriodEpochs, genesis.Params.MaxVotingPeriodEpochs)
	require.Equal(t, types.DefaultParams().DefaultVotingPeriodEpochs, genesis.Params.DefaultVotingPeriodEpochs)
	require.Equal(t, types.DefaultParams().BlocksPerEpoch, genesis.Params.BlocksPerEpoch)
	require.Equal(t, types.DefaultParams().TleEnabled, genesis.Params.TleEnabled)
	require.Equal(t, types.DefaultParams().TleThresholdNumerator, genesis.Params.TleThresholdNumerator)
	require.Equal(t, types.DefaultParams().TleThresholdDenominator, genesis.Params.TleThresholdDenominator)
	require.Equal(t, types.DefaultParams().OpenRegistration, genesis.Params.OpenRegistration)
	require.Equal(t, types.DefaultParams().MaxProposalsPerEpoch, genesis.Params.MaxProposalsPerEpoch)
}

func TestWeightedOperations(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(vote.AppModule{})
	am := vote.NewAppModule(encCfg.Codec, keeper.Keeper{}, nil, nil)
	simState := module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
		TxConfig:  encCfg.TxConfig,
	}

	ops := am.WeightedOperations(simState)
	require.Len(t, ops, 12)
}

func TestProposalMsgs(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(vote.AppModule{})
	am := vote.NewAppModule(encCfg.Codec, keeper.Keeper{}, nil, nil)
	simState := module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
	}

	msgs := am.ProposalMsgs(simState)
	require.Len(t, msgs, 0)
}
