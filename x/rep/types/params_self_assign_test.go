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

// The self-assigned gates are a floor on the number of *independent* stakers as
// much as they are magnitudes, because max_conviction_share_per_member caps what
// any one member contributes: a gate at `ratio` needs ceil(ratio / cap) stakers.
//
// There are TWO gates in CanCompleteInitiative. The obvious one is
// self_assigned_external_conviction_ratio, which floors the number of *external*
// stakers. The one that is easy to miss is `current >= required`: it counts
// affiliated stake as well, so it is not an external floor -- but when no
// affiliate stakes, and on self-assigned work none need to, the same external
// members have to carry it alone at an implicit ratio of 1.00.
//
// That second floor is what bit. At a 0.33 cap three unaided external stakers
// cleared the 0.75 ratio and then stopped dead at 0.99 of required, so moving
// the explicit ratio 1.00 -> 0.75 relaxed only the gate that was not binding for
// them. Raising the cap 0.33 -> 0.35 is what made three unaided stakers enough.
//
// Both floors count direct stakers only: conviction propagated from linked
// content is added uncapped, and can supply part of a threshold without them.
//
// Params.Validate enforces this band at the msg boundary; this test pins the
// defaults inside it. The cap has to stay in [1/3, 0.375): below 1/3 the total
// gate goes back to four stakers, and at 0.375 or above two stakers clear the
// 0.75 ratio, collapsing the self-assigned floor to the same two as ordinary
// externally-assigned work -- which erases the safeguard instead of relaxing
// it.
func TestSelfAssignedGatesImplyThreeIndependentStakers(t *testing.T) {
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

	require.Equal(t, int64(3), floor(math.LegacyOneDec()),
		"the total-conviction gate must be reachable by three independent stakers")
	require.Equal(t, int64(3), floor(p.SelfAssignedExternalConvictionRatio),
		"self-assigned work must need three independent stakers, not two or four")
	require.Equal(t, int64(2), floor(p.ExternalConvictionRatio),
		"ordinary externally-assigned work needs two")

	// The band, stated directly rather than left implicit in the floors above.
	require.True(t, p.MaxConvictionSharePerMember.MulInt64(3).GTE(math.LegacyOneDec()),
		"three stakers at the cap must cover the whole threshold")
	require.True(t, p.MaxConvictionSharePerMember.MulInt64(2).LT(p.SelfAssignedExternalConvictionRatio),
		"two stakers at the cap must not clear the self-assigned floor")
}

// The band the comment above describes is enforced, not just documented: the
// cap is an operational parameter and the ratio it is coupled to is not, so the
// Operations Committee can reach one half of the pairing on its own. Both
// entry points are covered -- MsgUpdateOperationalParams validates the
// operational params first and the merged Params second.
func TestConvictionShareCapBandIsEnforced(t *testing.T) {
	base := types.DefaultParams()

	for _, tc := range []struct {
		name    string
		share   math.LegacyDec
		wantErr bool
	}{
		{"default sits inside the band", base.MaxConvictionSharePerMember, false},
		// A third is not representable in 18 decimals, and the property the
		// check enforces is the exact one: three stakers must *cover* the
		// threshold. 0.333...333 leaves them at 0.999...999 and is rejected;
		// the next representable value up is the real lower edge.
		{"smallest cap three stakers can cover with", math.LegacyNewDecWithPrec(333333333333333334, 18), false},
		{"one third truncated leaves three stakers short", math.LegacyOneDec().QuoInt64(3), true},
		{"just under the upper edge", math.LegacyNewDecWithPrec(374, 3), false},
		{"below one third strands three stakers", math.LegacyNewDecWithPrec(33, 2), true},
		{"at 0.375 two stakers clear the self-assigned floor", math.LegacyNewDecWithPrec(375, 3), true},
		{"a cap of one erases the floor entirely", math.LegacyOneDec(), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			p.MaxConvictionSharePerMember = tc.share
			if tc.wantErr {
				require.Error(t, p.Validate())
			} else {
				require.NoError(t, p.Validate())
			}
		})
	}

	t.Run("operational params reject the lower edge before the merge", func(t *testing.T) {
		op := types.DefaultRepOperationalParams()
		op.MaxConvictionSharePerMember = math.LegacyNewDecWithPrec(33, 2)
		require.Error(t, op.Validate())
	})

	// The upper edge needs the governance-only ratio, so it can only be caught
	// once the operational params have been merged into the full set.
	t.Run("upper edge is caught after the merge", func(t *testing.T) {
		op := types.DefaultRepOperationalParams()
		op.MaxConvictionSharePerMember = math.LegacyNewDecWithPrec(40, 2)
		require.NoError(t, op.Validate())
		require.Error(t, base.ApplyOperationalParams(op).Validate())
	})

	// Equal ratios are a legitimate way to run without the safeguard, and the
	// upper edge must not turn that choice into an invalid parameter set.
	t.Run("no upper edge when no stricter ratio was asked for", func(t *testing.T) {
		p := base
		p.SelfAssignedExternalConvictionRatio = p.ExternalConvictionRatio
		p.MaxConvictionSharePerMember = math.LegacyOneDec()
		require.NoError(t, p.Validate())
	})
}
