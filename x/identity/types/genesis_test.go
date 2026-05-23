package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/identity/types"
)

// validIdentity returns a known-good ChainIdentity for genesis tests.
func validIdentity() types.ChainIdentity {
	return types.ChainIdentity{
		ChainHumanName:       "Phoenix",
		ChainTickerPrefix:    "PHX",
		BondDenom:            "upspk.phoenix",
		BondDisplaySymbol:    "PSPK",
		BondDisplayName:      "Phoenix Spark",
		BondDisplayDecimals:  6,
		DreamDenom:           "udream.phoenix",
		DreamDisplaySymbol:   "PDRM",
		DreamDisplayName:     "Phoenix Dream",
		DreamDisplayDecimals: 6,
		FoundedAt:            1735689600,
	}
}

func TestGenesisStateValidate(t *testing.T) {
	tests := []struct {
		desc  string
		gs    types.GenesisState
		valid bool
	}{
		{
			desc:  "valid identity",
			gs:    types.GenesisState{Identity: validIdentity()},
			valid: true,
		},
		{
			desc:  "default (empty identity) is accepted as legacy single-chain mode",
			gs:    *types.DefaultGenesis(),
			valid: true,
		},
		{
			desc:  "zero identity accepted (legacy mode)",
			gs:    types.GenesisState{},
			valid: true,
		},
		{
			desc:  "partial identity rejected (BondDenom set, DreamDenom unset)",
			gs:    types.GenesisState{Identity: types.ChainIdentity{BondDenom: "upspk.phoenix"}},
			valid: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			err := tc.gs.Validate()
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
