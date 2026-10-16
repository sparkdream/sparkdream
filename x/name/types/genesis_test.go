package types_test

import (
	"testing"

	"sparkdream/x/name/types"

	"cosmossdk.io/math"
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

func TestGenesisState_ValidateStakes(t *testing.T) {
	valid := func() types.GenesisState {
		gs := *types.DefaultGenesis()
		gs.DisputeStakes = []types.DisputeStake{{ChallengeId: "name_dispute:phoenix:1", Staker: "aurora", Amount: math.NewInt(5)}}
		gs.ContestStakes = []types.ContestStake{{ChallengeId: "name_contest:phoenix:2", Owner: "zenith", Amount: math.NewInt(5)}}
		return gs
	}
	require.NoError(t, valid().Validate())

	dup := valid()
	dup.DisputeStakes = append(dup.DisputeStakes, dup.DisputeStakes[0])
	require.Error(t, dup.Validate(), "duplicate dispute stake challenge id")

	dupC := valid()
	dupC.ContestStakes = append(dupC.ContestStakes, dupC.ContestStakes[0])
	require.Error(t, dupC.Validate(), "duplicate contest stake challenge id")

	neg := valid()
	neg.DisputeStakes[0].Amount = math.NewInt(-1)
	require.Error(t, neg.Validate(), "negative dispute stake")

	nilAmt := valid()
	nilAmt.ContestStakes[0].Amount = math.Int{}
	require.Error(t, nilAmt.Validate(), "nil contest stake amount")
}
