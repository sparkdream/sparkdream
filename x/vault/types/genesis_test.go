package types_test

import (
	"testing"

	"sparkdream/x/vault/types"

	"github.com/stretchr/testify/require"
)

func TestGenesisState_Validate(t *testing.T) {
	tests := []struct {
		desc     string
		genState *types.GenesisState
		valid    bool
	}{
		{
			desc:     "default is valid",
			genState: types.DefaultGenesis(),
			valid:    true,
		},
		{
			desc:     "valid genesis state",
			genState: &types.GenesisState{Params: types.DefaultParams(), VaultMap: []types.Vault{{VaultId: 0}, {VaultId: 1}}, VaultProposalMap: []types.VaultProposal{{ProposalId: 0}, {ProposalId: 1}}},
			valid:    true,
		}, {
			desc: "duplicated vault",
			genState: &types.GenesisState{
				VaultMap: []types.Vault{
					{
						VaultId: 0,
					},
					{
						VaultId: 0,
					},
				},
				VaultProposalMap: []types.VaultProposal{{ProposalId: 0}, {ProposalId: 1}}},
			valid: false,
		}, {
			desc: "duplicated vaultProposal",
			genState: &types.GenesisState{
				VaultProposalMap: []types.VaultProposal{
					{
						ProposalId: 0,
					},
					{
						ProposalId: 0,
					},
				},
			},
			valid: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			err := tc.genState.Validate()
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
