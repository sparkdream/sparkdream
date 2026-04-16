package types_test

import (
	"testing"

	"sparkdream/x/split/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
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
			genState: &types.GenesisState{ShareMap: []types.Share{
				{Address: sdk.AccAddress([]byte("share_addr_1________")).String()},
				{Address: sdk.AccAddress([]byte("share_addr_2________")).String()},
			}},
			valid: true,
		}, {
			desc: "duplicated share",
			genState: &types.GenesisState{
				ShareMap: []types.Share{
					{
						Address: sdk.AccAddress([]byte("share_addr_dup______")).String(),
					},
					{
						Address: sdk.AccAddress([]byte("share_addr_dup______")).String(),
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
