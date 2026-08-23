package types_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// jury_size is editable by the Operations Committee, and validation used to
// check only that it was odd. Two odd values were broken: at jury_size ==
// MinSeatedJurors the redraw sweep's `vacatable` is always zero, so the
// acceptance window, redraws and replacement selection are all dead code; and
// below the floor a jury can never reach quorum, so every challenge escalates.
// devnet and testnet shipped jury_size 3 against a floor of 3, so this was live.
func TestJurySizeMustExceedSeatedJuryFloor(t *testing.T) {
	for _, size := range []uint32{1, 3} {
		p := types.DefaultParams()
		p.JurySize = size
		require.Error(t, p.Validate(),
			"jury size %d is odd but leaves the redraw sweep no headroom", size)

		op := types.DefaultRepOperationalParams()
		op.JurySize = size
		require.Error(t, op.Validate(),
			"the committee must not be able to set jury size %d either", size)
	}

	// 5 is the shipped value and must keep validating on both surfaces.
	p := types.DefaultParams()
	p.JurySize = 5
	require.NoError(t, p.Validate())
	op := types.DefaultRepOperationalParams()
	op.JurySize = 5
	require.NoError(t, op.Validate())
}

// Each redraw round costs one acceptance window out of the review period, so a
// wide window and several rounds cannot both be configured.
func TestAcceptanceWindowAndRedrawsMustFitTheReviewPeriod(t *testing.T) {
	p := types.DefaultParams()
	p.JuryAcceptanceWindowRatio = math.LegacyNewDecWithPrec(5, 1) // 0.5
	p.MaxJuryRedraws = 2                                          // 3 windows = 1.5x the period
	require.Error(t, p.Validate())

	p.MaxJuryRedraws = 1 // 2 windows = exactly the period, still too much
	require.Error(t, p.Validate())

	p.JuryAcceptanceWindowRatio = math.LegacyNewDecWithPrec(25, 2) // 0.25
	p.MaxJuryRedraws = 1                                           // 2 windows = half the period
	require.NoError(t, p.Validate())
}

// Every parameter added for the reviewer role and the juror-pay rework needs
// its bounds pinned: each one is either an economic lever or a liveness lever,
// and the defaults are what three networks ship.
func TestReviewerAndJurorParamDefaults(t *testing.T) {
	p := types.DefaultParams()
	require.NoError(t, p.Validate())

	require.Equal(t, "0.100000000000000000", p.ReviewerBondReserveRate.String())
	require.Equal(t, "0.050000000000000000", p.ReviewFeeRate.String())
	require.Equal(t, uint32(3), p.MaxReviewRounds)
	require.Equal(t, "5000000", p.MinJurorReward.String())
	require.Equal(t, "0.100000000000000000", p.MinJurorSelectionWeight.String())
	require.Equal(t, uint64(3), p.MinJurySeatingsForWeighting)
	require.Equal(t, "0.100000000000000000", p.InitiativeCompletionBonusRate.String())

	// The operational mirror has to agree, or a committee update would silently
	// move a value away from what governance seeded.
	op := types.DefaultRepOperationalParams()
	require.NoError(t, op.Validate())
	require.Equal(t, p.ReviewerBondReserveRate, op.ReviewerBondReserveRate)
	require.Equal(t, p.ReviewFeeRate, op.ReviewFeeRate)
	require.Equal(t, p.MaxReviewRounds, op.MaxReviewRounds)
	require.Equal(t, p.MinJurorReward, op.MinJurorReward)
}

func TestReviewerParamBounds(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*types.Params)
	}{
		{"zero bond reserve leaves a reviewer nothing at risk", func(p *types.Params) {
			p.ReviewerBondReserveRate = math.LegacyZeroDec()
		}},
		{"bond reserve above 1 exceeds the budget", func(p *types.Params) {
			p.ReviewerBondReserveRate = math.LegacyNewDec(2)
		}},
		{"negative review fee", func(p *types.Params) {
			p.ReviewFeeRate = math.LegacyNewDecWithPrec(-1, 1)
		}},
		{"review fee above 1 exceeds the budget", func(p *types.Params) {
			p.ReviewFeeRate = math.LegacyNewDec(2)
		}},
		{"zero rounds makes a rejection unremediable", func(p *types.Params) {
			p.MaxReviewRounds = 0
		}},
		{"negative juror reward floor", func(p *types.Params) {
			p.MinJurorReward = math.NewInt(-1)
		}},
		{"zero selection weight is exclusion in all but name", func(p *types.Params) {
			p.MinJurorSelectionWeight = math.LegacyZeroDec()
		}},
		{"completion bonus above 1", func(p *types.Params) {
			p.InitiativeCompletionBonusRate = math.LegacyNewDec(2)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := types.DefaultParams()
			tc.mutate(&p)
			require.Error(t, p.Validate())
		})
	}
}
