package types_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

func TestSelfAssignParams_Defaults(t *testing.T) {
	p := types.DefaultParams()
	require.Equal(t, math.LegacyNewDecWithPrec(10, 2), p.SelfAssignedBondRate)
	require.Equal(t, math.LegacyNewDecWithPrec(75, 2), p.SelfAssignedExternalConvictionRatio)
	require.Equal(t, int64(2), p.SelfAssignedChallengeMultiplier)
	require.NoError(t, p.Validate())
}

func TestSelfAssignParams_ValidateBounds(t *testing.T) {
	base := types.DefaultParams()

	t.Run("bond rate", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			rate    math.LegacyDec
			wantErr bool
		}{
			{"zero disables the bond", math.LegacyZeroDec(), false},
			{"full budget is allowed", math.LegacyOneDec(), false},
			{"negative is rejected", math.LegacyNewDec(-1), true},
			{"above 1 is rejected", math.LegacyNewDec(2), true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				p := base
				p.SelfAssignedBondRate = tc.rate
				err := p.Validate()
				if tc.wantErr {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
			})
		}
	})

	t.Run("external conviction ratio", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			ratio   math.LegacyDec
			wantErr bool
		}{
			{"equal to base ratio is allowed", base.ExternalConvictionRatio, false},
			{"one is allowed", math.LegacyOneDec(), false},
			{"below base ratio is rejected", base.ExternalConvictionRatio.Sub(math.LegacyNewDecWithPrec(1, 2)), true},
			{"above 1 is rejected", math.LegacyNewDecWithPrec(101, 2), true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				p := base
				p.SelfAssignedExternalConvictionRatio = tc.ratio
				err := p.Validate()
				if tc.wantErr {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
			})
		}
	})

	t.Run("challenge multiplier", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			mult    int64
			wantErr bool
		}{
			{"one (no extension) is allowed", 1, false},
			{"two is allowed", 2, false},
			{"zero is rejected", 0, true},
			{"negative is rejected", -1, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				p := base
				p.SelfAssignedChallengeMultiplier = tc.mult
				err := p.Validate()
				if tc.wantErr {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
			})
		}
	})
}

// The self-assigned ratio is a floor on the number of *independent* stakers as
// much as it is a magnitude, because max_conviction_share_per_member caps what
// any one member contributes: the floor is ceil(ratio / cap). At 1.00 that
// floor was 4, which made the gate unreachable on a small chain rather than
// merely demanding -- three members maxed out reach 0.99 and stop.
//
// Pinned because the two params are tuned independently and the coupling is
// invisible from either one alone. Raising the cap past 0.375, or dropping the
// ratio to 0.66, silently collapses this floor to 2 -- the same as ordinary
// externally-assigned work, which erases the safeguard instead of relaxing it.
func TestSelfAssignedRatioImpliesThreeIndependentStakers(t *testing.T) {
	p := types.DefaultParams()

	floor := func(ratio math.LegacyDec) int64 {
		// Smallest n with n*cap >= ratio.
		for n := int64(1); n <= 10; n++ {
			if p.MaxConvictionSharePerMember.MulInt64(n).GTE(ratio) {
				return n
			}
		}
		return 0
	}

	require.Equal(t, int64(3), floor(p.SelfAssignedExternalConvictionRatio),
		"self-assigned work must need three independent stakers, not two or four")
	require.Equal(t, int64(2), floor(p.ExternalConvictionRatio),
		"ordinary externally-assigned work needs two")
	require.Equal(t, int64(4), floor(math.LegacyOneDec()),
		"and a ratio of 1.0 would need four -- the value this default moved off")
}
