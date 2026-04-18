package types_test

import (
	"testing"

	"sparkdream/x/forum/types"

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
				PostMap: []types.Post{
					{PostId: 0, CategoryId: 0, Status: types.PostStatus_POST_STATUS_ACTIVE},
					{PostId: 1, CategoryId: 1, Status: types.PostStatus_POST_STATUS_ACTIVE},
				},
				UserRateLimitMap: []types.UserRateLimit{{UserAddress: "addr0"}, {UserAddress: "addr1"}},
				BountyList:       []types.Bounty{{Id: 0, ThreadId: 0}, {Id: 1, ThreadId: 1}},
				BountyCount:      2,
			},
			valid: true,
		},
		{
			desc: "duplicated post",
			genState: &types.GenesisState{
				PostMap: []types.Post{
					{PostId: 0},
					{PostId: 0},
				},
			},
			valid: false,
		},
		{
			desc: "duplicated bounty",
			genState: &types.GenesisState{
				BountyList: []types.Bounty{
					{Id: 0},
					{Id: 0},
				},
			},
			valid: false,
		},
		{
			desc: "invalid bounty count",
			genState: &types.GenesisState{
				BountyList: []types.Bounty{
					{Id: 1},
				},
				BountyCount: 0,
			},
			valid: false,
		},
		{
			desc: "duplicated memberWarning",
			genState: &types.GenesisState{
				MemberWarningList: []types.MemberWarning{
					{Id: 0},
					{Id: 0},
				},
			},
			valid: false,
		},
		{
			desc: "invalid memberWarning count",
			genState: &types.GenesisState{
				MemberWarningList: []types.MemberWarning{
					{Id: 1},
				},
				MemberWarningCount: 0,
			},
			valid: false,
		},
		{
			desc: "duplicated govActionAppeal",
			genState: &types.GenesisState{
				GovActionAppealList: []types.GovActionAppeal{
					{Id: 0},
					{Id: 0},
				},
			},
			valid: false,
		},
		{
			desc: "invalid govActionAppeal count",
			genState: &types.GenesisState{
				GovActionAppealList: []types.GovActionAppeal{
					{Id: 1},
				},
				GovActionAppealCount: 0,
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
