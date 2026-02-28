package forum_test

import (
	"encoding/json"
	"math/rand"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/module"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/stretchr/testify/require"

	forum "sparkdream/x/forum/module"
	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestGenerateGenesisState(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(forum.AppModule{})
	simState := &module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
		Rand:      rand.New(rand.NewSource(1)),
		GenState:  make(map[string]json.RawMessage),
		Accounts:  simtypes.RandomAccounts(rand.New(rand.NewSource(1)), 3),
	}
	forum.AppModule{}.GenerateGenesisState(simState)

	raw, ok := simState.GenState[types.ModuleName]
	require.True(t, ok, "expected GenState to contain entry for module %q", types.ModuleName)

	var genesis types.GenesisState
	require.NoError(t, encCfg.Codec.UnmarshalJSON(raw, &genesis))

	// JSON round-trip normalizes nil slices to empty slices; normalize the
	// expected value so the comparison is not sensitive to nil vs [].
	expected := types.DefaultParams()
	if expected.AnonSubsidyApprovedRelays == nil {
		expected.AnonSubsidyApprovedRelays = []string{}
	}
	require.Equal(t, expected, genesis.Params)

	// Verify simulation tags are pre-seeded for cross-module tag validation
	require.NotEmpty(t, genesis.TagMap, "simulation genesis should pre-seed tags")
	tagNames := make(map[string]bool)
	for _, tag := range genesis.TagMap {
		tagNames[tag.Name] = true
	}
	// Spot-check tags used by x/rep and x/collect simulations
	require.True(t, tagNames["backend"], "should include rep simulation tag 'backend'")
	require.True(t, tagNames["design"], "should include shared simulation tag 'design'")
	require.True(t, tagNames["art"], "should include collect simulation tag 'art'")
}

func TestWeightedOperations(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(forum.AppModule{})
	am := forum.NewAppModule(encCfg.Codec, keeper.Keeper{}, nil, nil)
	simState := module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
		TxConfig:  encCfg.TxConfig,
	}
	ops := am.WeightedOperations(simState)
	require.Len(t, ops, 51)
}

func TestProposalMsgs(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(forum.AppModule{})
	am := forum.NewAppModule(encCfg.Codec, keeper.Keeper{}, nil, nil)
	simState := module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
	}
	msgs := am.ProposalMsgs(simState)
	require.Len(t, msgs, 0)
}
