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
		// CMN-15: the group index is the collection key and the policy
		// indexes are rebuilt from group_map.
		{
			desc:     "group with empty index",
			genState: &types.GenesisState{Params: types.DefaultParams(), GroupMap: []types.Group{{Index: "", PolicyAddress: sampleAddr}}},
			valid:    false,
		},
		{
			desc: "two groups claim the same policy address",
			genState: &types.GenesisState{Params: types.DefaultParams(), GroupMap: []types.Group{
				{Index: "Aurora Guild", PolicyAddress: sampleAddr},
				{Index: "Zenith Circle", PolicyAddress: sampleAddr2, VetoPolicyAddress: sampleAddr},
			}},
			valid: false,
		},
		{
			desc: "members of an unknown group",
			genState: &types.GenesisState{Params: types.DefaultParams(),
				GroupMap:       []types.Group{{Index: "Aurora Guild", PolicyAddress: sampleAddr}},
				CouncilMembers: []types.CouncilMembers{{CouncilName: "Zenith Circle"}},
			},
			valid: false,
		},
		{
			desc: "groups with distinct policies and their members",
			genState: &types.GenesisState{Params: types.DefaultParams(),
				GroupMap: []types.Group{
					{Index: "Aurora Guild", PolicyAddress: sampleAddr},
					{Index: "Zenith Circle", PolicyAddress: sampleAddr2},
				},
				CouncilMembers: []types.CouncilMembers{{CouncilName: "Zenith Circle"}},
			},
			valid: true,
		},
		// CMN-17: anonymous state.
		{
			desc: "duplicate anon vote tally",
			genState: &types.GenesisState{Params: types.DefaultParams(),
				AnonVoteTallies: []types.AnonVoteTallyEntry{{ProposalId: 1}, {ProposalId: 1}},
			},
			valid: false,
		},
		{
			desc: "duplicate anon proposal counter",
			genState: &types.GenesisState{Params: types.DefaultParams(),
				AnonProposalCounts: []types.AnonProposalCountEntry{
					{CouncilName: "Aurora Guild", UtcDay: 3, Count: 1},
					{CouncilName: "Aurora Guild", UtcDay: 3, Count: 2},
				},
			},
			valid: false,
		},
		{
			desc: "anon counters on distinct days",
			genState: &types.GenesisState{Params: types.DefaultParams(),
				AnonProposalCounts: []types.AnonProposalCountEntry{
					{CouncilName: "Aurora Guild", UtcDay: 3, Count: 1},
					{CouncilName: "Aurora Guild", UtcDay: 4, Count: 2},
				},
			},
			valid: true,
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
