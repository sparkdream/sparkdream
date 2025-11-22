package types_test

import (
	"testing"

	"sparkdream/x/name/types"

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
			desc: "valid genesis state",
			genState: &types.GenesisState{
				Params: types.DefaultParams(),
				DisputeMap: []types.Dispute{
					{Name: "0", Claimant: "0"},
					{Name: "1", Claimant: "1"},
				},
				NameRecords: []types.NameRecord{
					{Name: "0", Owner: "0", Data: "0"},
					{Name: "1", Owner: "1", Data: "1"},
				},
				OwnerInfos: []types.OwnerInfo{
					{Address: "0", PrimaryName: "0"},
					{Address: "1", PrimaryName: "1"},
				},
			},
			valid: true,
		},
		{
			desc: "duplicated dispute",
			genState: &types.GenesisState{
				Params: types.DefaultParams(),
				DisputeMap: []types.Dispute{
					{Name: "0", Claimant: "0"},
					{Name: "0", Claimant: "1"},
				},
			},
			valid: false,
		},
		{
			desc: "duplicated name record",
			genState: &types.GenesisState{
				Params: types.DefaultParams(),
				NameRecords: []types.NameRecord{
					{Name: "0", Owner: "0"},
					{Name: "0", Owner: "1"},
				},
			},
			valid: false,
		},
		{
			desc: "duplicated owner info",
			genState: &types.GenesisState{
				Params: types.DefaultParams(),
				OwnerInfos: []types.OwnerInfo{
					{Address: "0", PrimaryName: "0"},
					{Address: "0", PrimaryName: "1"},
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
