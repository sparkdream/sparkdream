package commons_test

import (
	"encoding/json"
	"math/rand"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/module"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/stretchr/testify/require"

	"sparkdream/x/commons/keeper"
	commons "sparkdream/x/commons/module"
	"sparkdream/x/commons/types"
)

func TestGenerateGenesisState(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(commons.AppModule{})
	simState := &module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
		Rand:      rand.New(rand.NewSource(1)),
		GenState:  make(map[string]json.RawMessage),
		Accounts:  simtypes.RandomAccounts(rand.New(rand.NewSource(1)), 3),
	}
	commons.AppModule{}.GenerateGenesisState(simState)

	raw, ok := simState.GenState[types.ModuleName]
	require.True(t, ok, "expected GenState to contain entry for module %q", types.ModuleName)

	var genesis types.GenesisState
	require.NoError(t, encCfg.Codec.UnmarshalJSON(raw, &genesis))

	require.Equal(t, types.DefaultParams(), genesis.Params)
	require.NoError(t, genesis.Validate())
	// Three accounts seat only the founder (two are always left unseated);
	// no group is imported, so InitGenesis bootstraps the councils around it.
	require.Len(t, genesis.FoundingMembers, 1)
	require.True(t, genesis.FoundingMembers[0].Founder)
	require.Empty(t, genesis.GroupMap)
	require.Empty(t, genesis.CouncilMembers)

	// With enough accounts five founders are seated and every staffed
	// committee gets two to five members, all of them founders.
	simState.Accounts = simtypes.RandomAccounts(rand.New(rand.NewSource(2)), 20)
	simState.GenState = make(map[string]json.RawMessage)
	commons.AppModule{}.GenerateGenesisState(simState)
	genesis = types.GenesisState{}
	require.NoError(t, encCfg.Codec.UnmarshalJSON(simState.GenState[types.ModuleName], &genesis))
	require.NoError(t, genesis.Validate())
	require.Len(t, genesis.FoundingMembers, 5)
	founders := map[string]bool{}
	for _, m := range genesis.FoundingMembers {
		founders[m.Address] = true
	}
	require.Len(t, genesis.CouncilMembers, len(commons.SimCommitteeNames))
	for _, cm := range genesis.CouncilMembers {
		// the founder is seated by the bootstrap, so 1-4 extra rows
		require.NotEmpty(t, cm.Members)
		require.LessOrEqual(t, len(cm.Members), 4)
		for _, m := range cm.Members {
			require.True(t, founders[m.Address], "committee seat for a non-founder")
		}
	}
}

func TestWeightedOperations(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(commons.AppModule{})
	am := commons.NewAppModule(encCfg.Codec, keeper.Keeper{}, nil, nil)
	simState := module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
		TxConfig:  encCfg.TxConfig,
	}
	ops := am.WeightedOperations(simState)
	// 14 operations after M10 (RecurringSpend migration; the 4
	// recurring-spend simulations were removed because they reached into
	// the deleted parallel storage, coverage moved to M11 e2e scripts),
	// plus the 4 poll messages.
	require.Len(t, ops, 18)
}

func TestProposalMsgs(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(commons.AppModule{})
	am := commons.NewAppModule(encCfg.Codec, keeper.Keeper{}, nil, nil)
	simState := module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
	}
	msgs := am.ProposalMsgs(simState)
	require.Len(t, msgs, 0)
}
