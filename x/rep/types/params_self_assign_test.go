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
	require.Equal(t, math.LegacyOneDec(), p.SelfAssignedExternalConvictionRatio)
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
