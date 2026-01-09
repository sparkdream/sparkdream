package types_test

import (
	"testing"

	"sparkdream/x/futarchy/types"

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
			// Initialize Params using DefaultParams(), otherwise MinLiquidity is 0 (invalid)
			genState: &types.GenesisState{
				Params:    types.DefaultParams(),
				MarketMap: []types.Market{{Index: 0}, {Index: 1}},
			},
			valid: true,
		},
		{
			desc: "duplicated market",
			genState: &types.GenesisState{
				// Params not strictly needed here because duplicate check happens first,
				// but good practice to include them.
				Params: types.DefaultParams(),
				MarketMap: []types.Market{
					{
						Index: 0,
					},
					{
						Index: 0,
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
