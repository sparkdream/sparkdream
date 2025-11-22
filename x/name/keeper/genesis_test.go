package keeper_test

import (
	"testing"

	"sparkdream/x/name/types"

	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),

		// Fix: Add valid NameRecords
		NameRecords: []types.NameRecord{
			{Name: "alice", Owner: "cosmos1alice", Data: "metadata"},
			{Name: "bob", Owner: "cosmos1bob", Data: "metadata"},
		},

		// Fix: Add valid OwnerInfos
		OwnerInfos: []types.OwnerInfo{
			{Address: "cosmos1alice", PrimaryName: "alice"},
			{Address: "cosmos1bob", PrimaryName: "bob"},
		},

		// Fix: Use valid Dispute fields (Name, Claimant) instead of 'Index'
		DisputeMap: []types.Dispute{
			{Name: "alice", Claimant: "cosmos1bob"},
			{Name: "bob", Claimant: "cosmos1charlie"},
		},
	}

	f := initFixture(t)
	err := f.keeper.InitGenesis(f.ctx, genesisState)
	require.NoError(t, err)

	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.EqualExportedValues(t, genesisState.Params, got.Params)
	// Verify all collections were exported correctly
	require.ElementsMatch(t, genesisState.NameRecords, got.NameRecords)
	require.ElementsMatch(t, genesisState.OwnerInfos, got.OwnerInfos)
	require.ElementsMatch(t, genesisState.DisputeMap, got.DisputeMap)
}
