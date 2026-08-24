package types_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// Params for the mandatory review gate and the bounties that fund it. Both
// halves of every param matter: the default has to be the value the mechanism
// was designed around, and Validate has to refuse the settings that would make
// the mechanism unsafe rather than merely badly tuned.

func TestReviewGateParams_Defaults(t *testing.T) {
	p := types.DefaultParams()
	require.NoError(t, p.Validate())

	// The threshold is the APPRENTICE ceiling, so apprentice work stays exempt
	// and every permissionless STANDARD initiative is gated. If either of these
	// drifts apart the gate silently changes which work it covers.
	require.Equal(t, p.ApprenticeTier.MaxBudget.String(), p.ReviewRequiredAboveBudget.String(),
		"the gate threshold tracks the apprentice ceiling by design")
	require.True(t, p.StandardTier.MaxBudget.GT(p.ReviewRequiredAboveBudget),
		"standard-tier work must fall above the gate")

	require.True(t, p.ReviewBountyReclaimDelay > 0,
		"a zero delay makes advertising a bounty and pulling it in the same block free")
	require.True(t, p.PermissionlessMinReviewBountyRate.IsPositive(),
		"permissionless work must pay for the review its own minting consumes")

	// The operational mirror must carry the same values, or a committee edit
	// silently resets them.
	op := types.DefaultRepOperationalParams()
	require.Equal(t, p.ReviewRequiredAboveBudget, op.ReviewRequiredAboveBudget)
	require.Equal(t, p.ReviewBountyReclaimDelay, op.ReviewBountyReclaimDelay)
	require.Equal(t, p.PermissionlessMinReviewBountyRate, op.PermissionlessMinReviewBountyRate)
}

func TestReviewGateParams_Validate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(p *types.Params)
		wantErr bool
	}{
		{"threshold zero disables the chain-wide gate",
			func(p *types.Params) { p.ReviewRequiredAboveBudget = math.ZeroInt() }, false},
		{"threshold negative",
			func(p *types.Params) { p.ReviewRequiredAboveBudget = math.NewInt(-1) }, true},
		{"threshold nil",
			func(p *types.Params) { p.ReviewRequiredAboveBudget = math.Int{} }, true},
		{"bounty rate zero disables the permissionless minimum",
			func(p *types.Params) { p.PermissionlessMinReviewBountyRate = math.LegacyZeroDec() }, false},
		{"bounty rate negative",
			func(p *types.Params) { p.PermissionlessMinReviewBountyRate = math.LegacyNewDec(-1) }, true},
		// Above 1 would charge more than the whole budget to have it reviewed,
		// which makes commissioning permissionless work strictly loss-making
		// and is far likelier to be a typo than an intent.
		{"bounty rate above one",
			func(p *types.Params) { p.PermissionlessMinReviewBountyRate = math.LegacyNewDecWithPrec(101, 2) }, true},
		{"bounty rate nil",
			func(p *types.Params) { p.PermissionlessMinReviewBountyRate = math.LegacyDec{} }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := types.DefaultParams()
			tc.mutate(&p)
			if tc.wantErr {
				require.Error(t, p.Validate())
			} else {
				require.NoError(t, p.Validate())
			}
		})
	}
}

func TestReviewGateOperationalParams_Validate(t *testing.T) {
	// The committee-editable mirror must reject everything Params rejects, or
	// the bound is only enforced on the path nobody uses.
	for _, tc := range []struct {
		name   string
		mutate func(op *types.RepOperationalParams)
	}{
		{"threshold negative", func(op *types.RepOperationalParams) {
			op.ReviewRequiredAboveBudget = math.NewInt(-1)
		}},
		{"bounty rate above one", func(op *types.RepOperationalParams) {
			op.PermissionlessMinReviewBountyRate = math.LegacyNewDec(2)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			op := types.DefaultRepOperationalParams()
			tc.mutate(&op)
			require.Error(t, op.Validate())
		})
	}
}

func TestReviewGateParams_SurviveApplyExtractRoundTrip(t *testing.T) {
	// The six-touch-point wiring is easy to half-finish: a param added to the
	// defaults and validation but missed in Extract reads back nil, which this
	// round trip catches.
	p := types.DefaultParams()
	p.ReviewRequiredAboveBudget = math.NewInt(777)
	p.ReviewBountyReclaimDelay = 999
	p.PermissionlessMinReviewBountyRate = math.LegacyNewDecWithPrec(25, 2)

	round := types.DefaultParams().ApplyOperationalParams(p.ExtractOperationalParams())

	require.Equal(t, p.ReviewRequiredAboveBudget, round.ReviewRequiredAboveBudget)
	require.Equal(t, p.ReviewBountyReclaimDelay, round.ReviewBountyReclaimDelay)
	require.Equal(t, p.PermissionlessMinReviewBountyRate, round.PermissionlessMinReviewBountyRate)
}
