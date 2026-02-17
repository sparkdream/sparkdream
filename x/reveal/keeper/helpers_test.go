package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/reveal/keeper"
	"sparkdream/x/reveal/types"
)

func TestVoteKey(t *testing.T) {
	tests := []struct {
		name     string
		contrib  uint64
		tranche  uint32
		voter    string
		expected string
	}{
		{"basic", 1, 0, "cosmos1abc", "1/0/cosmos1abc"},
		{"large ids", 99999, 10, "cosmos1xyz", "99999/10/cosmos1xyz"},
		{"zero values", 0, 0, "cosmos1", "0/0/cosmos1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := keeper.VoteKey(tc.contrib, tc.tranche, tc.voter)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestTrancheKey(t *testing.T) {
	tests := []struct {
		name     string
		contrib  uint64
		tranche  uint32
		expected string
	}{
		{"basic", 1, 0, "1/0"},
		{"larger ids", 42, 3, "42/3"},
		{"zero values", 0, 0, "0/0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := keeper.TrancheKey(tc.contrib, tc.tranche)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestEffectiveMinVotes(t *testing.T) {
	tests := []struct {
		name           string
		minVotes       uint32
		stakeThreshold math.Int
		expected       uint32
	}{
		{"base minimum used", 3, math.NewInt(10000), 3},
		{"scaled exceeds base", 3, math.NewInt(20000), 4},
		{"scaled exactly at base", 3, math.NewInt(15000), 3},
		{"high threshold", 3, math.NewInt(100000), 20},
		{"low threshold", 5, math.NewInt(5000), 5},
		{"zero threshold", 3, math.NewInt(0), 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := keeper.EffectiveMinVotes(tc.minVotes, tc.stakeThreshold)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestGetTranche(t *testing.T) {
	contrib := &types.Contribution{
		Tranches: []types.RevealTranche{
			{Id: 0, Name: "first"},
			{Id: 1, Name: "second"},
		},
	}

	t.Run("valid tranche 0", func(t *testing.T) {
		tranche, err := keeper.GetTranche(contrib, 0)
		require.NoError(t, err)
		require.Equal(t, "first", tranche.Name)
	})

	t.Run("valid tranche 1", func(t *testing.T) {
		tranche, err := keeper.GetTranche(contrib, 1)
		require.NoError(t, err)
		require.Equal(t, "second", tranche.Name)
	})

	t.Run("out of bounds", func(t *testing.T) {
		_, err := keeper.GetTranche(contrib, 2)
		require.ErrorIs(t, err, types.ErrTrancheNotFound)
	})

	t.Run("empty contribution", func(t *testing.T) {
		empty := &types.Contribution{}
		_, err := keeper.GetTranche(empty, 0)
		require.ErrorIs(t, err, types.ErrTrancheNotFound)
	})
}

func TestHasAnyTrancheReachedStatus(t *testing.T) {
	tests := []struct {
		name      string
		statuses  []types.TrancheStatus
		minStatus types.TrancheStatus
		expected  bool
	}{
		{
			"all locked below backed",
			[]types.TrancheStatus{
				types.TrancheStatus_TRANCHE_STATUS_LOCKED,
				types.TrancheStatus_TRANCHE_STATUS_STAKING,
			},
			types.TrancheStatus_TRANCHE_STATUS_BACKED,
			false,
		},
		{
			"one at backed",
			[]types.TrancheStatus{
				types.TrancheStatus_TRANCHE_STATUS_LOCKED,
				types.TrancheStatus_TRANCHE_STATUS_BACKED,
			},
			types.TrancheStatus_TRANCHE_STATUS_BACKED,
			true,
		},
		{
			"one beyond backed",
			[]types.TrancheStatus{
				types.TrancheStatus_TRANCHE_STATUS_LOCKED,
				types.TrancheStatus_TRANCHE_STATUS_REVEALED,
			},
			types.TrancheStatus_TRANCHE_STATUS_BACKED,
			true,
		},
		{
			"empty tranches",
			[]types.TrancheStatus{},
			types.TrancheStatus_TRANCHE_STATUS_BACKED,
			false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			contrib := &types.Contribution{}
			for _, s := range tc.statuses {
				contrib.Tranches = append(contrib.Tranches, types.RevealTranche{Status: s})
			}
			result := keeper.HasAnyTrancheReachedStatus(contrib, tc.minStatus)
			require.Equal(t, tc.expected, result)
		})
	}
}
