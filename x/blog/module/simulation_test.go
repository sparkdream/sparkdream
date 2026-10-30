package blog_test

import (
	"encoding/json"
	"math/rand"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/module"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	blog "sparkdream/x/blog/module"
	"sparkdream/x/blog/types"
)

func TestGenerateGenesisState(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(blog.AppModule{})
	simState := &module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
		Rand:      rand.New(rand.NewSource(1)),
		GenState:  make(map[string]json.RawMessage),
		Accounts:  simtypes.RandomAccounts(rand.New(rand.NewSource(1)), 3),
	}
	blog.AppModule{}.GenerateGenesisState(simState)

	raw, ok := simState.GenState[types.ModuleName]
	require.True(t, ok, "expected GenState to contain entry for module %q", types.ModuleName)

	var genesis types.GenesisState
	require.NoError(t, encCfg.Codec.UnmarshalJSON(raw, &genesis))

	// The media scan fee is randomized; every other param is the default.
	defaultParams := types.DefaultParams()
	require.True(t, genesis.Params.MediaScanFee.IsZero() || genesis.Params.MediaScanFee.IsPositive())
	genesis.Params.MediaScanFee = defaultParams.MediaScanFee
	require.Equal(t, defaultParams, genesis.Params)
	require.Equal(t, uint64(1), genesis.PostCount)
	require.Equal(t, uint64(1), genesis.ReplyCount)
}

func TestWeightedOperations(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(blog.AppModule{})
	am := blog.NewAppModule(encCfg.Codec, keeper.Keeper{}, nil, nil)
	simState := module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
		TxConfig:  encCfg.TxConfig,
	}
	ops := am.WeightedOperations(simState)
	require.Len(t, ops, 19)
}

func TestProposalMsgs(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(blog.AppModule{})
	am := blog.NewAppModule(encCfg.Codec, keeper.Keeper{}, nil, nil)
	simState := module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
	}
	msgs := am.ProposalMsgs(simState)
	require.Len(t, msgs, 0)
}
