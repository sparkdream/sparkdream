package keeper_test

import (
	"testing"

	"sparkdream/x/vault/types"

	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params:   types.DefaultParams(),
		VaultMap: []types.Vault{{VaultId: 0}, {VaultId: 1}}, VaultProposalMap: []types.VaultProposal{{ProposalId: 0}, {ProposalId: 1}}}

	f := initFixture(t)
	err := f.keeper.InitGenesis(f.ctx, genesisState)
	require.NoError(t, err)
	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.EqualExportedValues(t, genesisState.Params, got.Params)
	require.EqualExportedValues(t, genesisState.VaultMap, got.VaultMap)
	require.EqualExportedValues(t, genesisState.VaultProposalMap, got.VaultProposalMap)

}
