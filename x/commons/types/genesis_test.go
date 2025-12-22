package types_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types" // <--- Added Import
	"github.com/stretchr/testify/require"

	"sparkdream/x/commons/types"
)

func TestGenesisState_Validate(t *testing.T) {
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("sprkdrm", "sprkdrmpub")

	// Sample valid address for testing (Alice's address)
	sampleAddr := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	sampleAddr2 := "sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y"

	tests := []struct {
		desc     string
		genState *types.GenesisState
		valid    bool
	}{
		{
			desc: "default is valid",
			genState: func() *types.GenesisState {
				gs := types.DefaultGenesis()
				return gs
			}(),
			valid: true,
		},
		{
			desc: "valid genesis state",
			genState: &types.GenesisState{
				Params: types.Params{
					ProposalFee: "1000stake",
				},
				PolicyPermissionsMap: []types.PolicyPermissions{
					{PolicyAddress: sampleAddr},
					{PolicyAddress: sampleAddr2},
				},
				ExtendedGroupMap: []types.ExtendedGroup{{Index: "0"}, {Index: "1"}}},
			valid: true,
		},
		{
			desc: "duplicated policyPermissions",
			genState: &types.GenesisState{
				Params: types.Params{
					ProposalFee: "1000stake",
				},
				PolicyPermissionsMap: []types.PolicyPermissions{
					{
						PolicyAddress: sampleAddr,
					},
					{
						PolicyAddress: sampleAddr, // Duplicate!
					},
				},
				ExtendedGroupMap: []types.ExtendedGroup{{Index: "0"}, {Index: "1"}}},
			valid: false,
		}, {
			desc: "duplicated extendedGroup",
			genState: &types.GenesisState{
				ExtendedGroupMap: []types.ExtendedGroup{
					{
						Index: "0",
					},
					{
						Index: "0",
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
