package types_test

import (
	"testing"

	"sparkdream/x/collect/types"

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
			},
			valid: true,
		},
		{
			desc: "one open hide per target, resolved ones unlimited",
			genState: &types.GenesisState{
				Params: types.DefaultParams(),
				HideRecords: []types.HideRecord{
					{Id: 1, TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, TargetId: 7, Resolved: true},
					{Id: 2, TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, TargetId: 7, Resolved: true},
					{Id: 3, TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, TargetId: 7},
					{Id: 4, TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_ITEM, TargetId: 7},
				},
			},
			valid: true,
		},
		{
			desc: "two open hides on one target",
			genState: &types.GenesisState{
				Params: types.DefaultParams(),
				HideRecords: []types.HideRecord{
					{Id: 1, TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, TargetId: 7},
					{Id: 2, TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, TargetId: 7, Appealed: true},
				},
			},
			valid: false,
		},
		{
			desc: "duplicate hide record id",
			genState: &types.GenesisState{
				Params: types.DefaultParams(),
				HideRecords: []types.HideRecord{
					{Id: 1, TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, TargetId: 7, Resolved: true},
					{Id: 1, TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, TargetId: 8, Resolved: true},
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
