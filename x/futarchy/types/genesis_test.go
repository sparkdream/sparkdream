package types_test

import (
	"testing"

	"cosmossdk.io/math"

	"sparkdream/x/futarchy/types"

	"github.com/stretchr/testify/require"
)

func validMarket(index uint64) types.Market {
	bv := math.LegacyOneDec()
	pool := math.ZeroInt()
	tick := math.OneInt()
	liq := math.ZeroInt()
	return types.Market{
		Index:              index,
		BValue:             &bv,
		PoolYes:            &pool,
		PoolNo:             &pool,
		MinTick:            &tick,
		InitialLiquidity:   &liq,
		LiquidityWithdrawn: &liq,
	}
}

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
				MarketMap: []types.Market{validMarket(0), validMarket(1)},
			},
			valid: true,
		},
		{
			desc: "duplicated market",
			genState: &types.GenesisState{
				// Params not strictly needed here because duplicate check happens first,
				// but good practice to include them.
				Params:    types.DefaultParams(),
				MarketMap: []types.Market{validMarket(0), validMarket(0)},
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
