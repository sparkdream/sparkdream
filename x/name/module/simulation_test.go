package name_test

import (
	"encoding/json"
	"math/rand"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/module"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/stretchr/testify/require"

	"sparkdream/x/name/keeper"
	name "sparkdream/x/name/module"
	"sparkdream/x/name/types"
)

func TestGenerateGenesisState(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(name.AppModule{})
	simState := &module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
		Rand:      rand.New(rand.NewSource(1)),
		GenState:  make(map[string]json.RawMessage),
		Accounts:  simtypes.RandomAccounts(rand.New(rand.NewSource(1)), 3),
	}
	name.AppModule{}.GenerateGenesisState(simState)

	raw, ok := simState.GenState[types.ModuleName]
	require.True(t, ok, "expected GenState to contain entry for module %q", types.ModuleName)

	var genesis types.GenesisState
	require.NoError(t, encCfg.Codec.UnmarshalJSON(raw, &genesis))

	require.NoError(t, genesis.Validate())
	// The simulation shortens the contest and inactivity windows so a short
	// run reaches them; everything else is the default.
	want := types.DefaultParams()
	want.DisputeTimeoutBlocks = genesis.Params.DisputeTimeoutBlocks
	want.ExpirationDuration = genesis.Params.ExpirationDuration
	require.Equal(t, want, genesis.Params)
	require.Less(t, genesis.Params.DisputeTimeoutBlocks, types.DefaultDisputeTimeoutBlocks)
	require.Less(t, genesis.Params.ExpirationDuration, types.DefaultExpirationDuration)
}

func TestWeightedOperations(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(name.AppModule{})
	am := name.NewAppModule(encCfg.Codec, keeper.Keeper{}, nil, nil)
	simState := module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
		TxConfig:  encCfg.TxConfig,
	}
	ops := am.WeightedOperations(simState)
	// One operation per user-facing name message (NAM-07): RegisterName,
	// SetPrimary, UpdateName, SetDisplayName, SetTarget, AcceptTarget,
	// TransferName, FileDispute, ContestDispute and ResolveDispute.
	require.Len(t, ops, 10)
}

func TestProposalMsgs(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(name.AppModule{})
	am := name.NewAppModule(encCfg.Codec, keeper.Keeper{}, nil, nil)
	simState := module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
	}
	msgs := am.ProposalMsgs(simState)
	require.Len(t, msgs, 0)
}
