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
				Params: types.NewParams("1000stake"),
				PolicyPermissionsMap: []types.PolicyPermissions{
					{PolicyAddress: sampleAddr},
					{PolicyAddress: sampleAddr2},
				},
				GroupMap: []types.Group{{Index: "0"}, {Index: "1"}}},
			valid: true,
		},
		{
			desc: "duplicated policyPermissions",
			genState: &types.GenesisState{
				Params: types.NewParams("1000stake"),
				PolicyPermissionsMap: []types.PolicyPermissions{
					{
						PolicyAddress: sampleAddr,
					},
					{
						PolicyAddress: sampleAddr, // Duplicate!
					},
				},
				GroupMap: []types.Group{{Index: "0"}, {Index: "1"}}},
			valid: false,
		}, {
			desc: "duplicated group",
			genState: &types.GenesisState{
				GroupMap: []types.Group{
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
		{
			desc: "valid founding members override",
			genState: &types.GenesisState{
				Params: types.DefaultParams(),
				FoundingMembers: []types.FoundingMember{
					{Address: sampleAddr, DisplayName: "Alice", Handles: []string{"alice"}, Founder: true},
					{Address: sampleAddr2, DisplayName: "Bob"},
				},
			},
			valid: true,
		},
		{
			desc: "founding members without a founder",
			genState: &types.GenesisState{
				Params: types.DefaultParams(),
				FoundingMembers: []types.FoundingMember{
					{Address: sampleAddr, DisplayName: "Alice"},
				},
			},
			valid: false,
		},
		{
			desc: "founding members with two founders",
			genState: &types.GenesisState{
				Params: types.DefaultParams(),
				FoundingMembers: []types.FoundingMember{
					{Address: sampleAddr, DisplayName: "Alice", Founder: true},
					{Address: sampleAddr2, DisplayName: "Bob", Founder: true},
				},
			},
			valid: false,
		},
		{
			desc: "founding member with invalid address",
			genState: &types.GenesisState{
				Params: types.DefaultParams(),
				FoundingMembers: []types.FoundingMember{
					{Address: "sprkdrm1notanaddress", DisplayName: "Alice", Founder: true},
				},
			},
			valid: false,
		},
		{
			desc: "duplicate founding member address",
			genState: &types.GenesisState{
				Params: types.DefaultParams(),
				FoundingMembers: []types.FoundingMember{
					{Address: sampleAddr, DisplayName: "Alice", Founder: true},
					{Address: sampleAddr, DisplayName: "Bob"},
				},
			},
			valid: false,
		},
		{
			desc: "founding member with empty display name",
			genState: &types.GenesisState{
				Params: types.DefaultParams(),
				FoundingMembers: []types.FoundingMember{
					{Address: sampleAddr, DisplayName: "", Founder: true},
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
