package types_test

import (
	"testing"

	"sparkdream/x/service/types"

	"github.com/stretchr/testify/require"
)

// TestGenesisState_Validate verifies the default genesis passes validation
// and that obviously-invalid params (e.g., zero block-window params) fail.
// Per §4.2 validation rules, every *_blocks param must be > 0 and the
// LegacyDec reputation field must be set; an empty Params{} fails on
// multiple counts.
func TestGenesisState_Validate(t *testing.T) {
	invalidParams := types.DefaultParams()
	invalidParams.MaxPendingBlocks = 0 // violates "*_blocks must be > 0"

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
			desc: "zero max_pending_blocks fails",
			genState: &types.GenesisState{
				Params: invalidParams,
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
