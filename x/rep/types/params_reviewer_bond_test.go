package types_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// The shipped floor is the number the UI quotes to anyone considering the role.
// It is deliberately low: per-verdict exposure is the reserve
// (reviewer_bond_reserve_rate x budget), not the floor, so a small floor keeps
// entry cheap without weakening accountability. Reviewers who want larger work
// bond above it.
func TestReviewerBondPolicy_Defaults(t *testing.T) {
	p := types.DefaultParams()
	require.Equal(t, math.NewInt(500_000_000), p.MinReviewerBond)
	require.Equal(t, math.NewInt(250_000_000), p.ReviewerDemotionThreshold)
	require.Equal(t, "TRUST_LEVEL_ESTABLISHED", p.MinReviewerTrustLevel)
	require.Equal(t, uint64(0), p.MinReviewerRepTier)
	require.Equal(t, int64(0), p.MinReviewerAgeBlocks)
	require.Equal(t, int64(604800), p.ReviewerDemotionCooldown)
	require.Equal(t, int64(1209600), p.ReviewerUnbondCooldown)
	require.NoError(t, p.Validate())
}

// Params and RepOperationalParams must describe the same policy, or the council
// path would advertise one thing and the gov path another.
func TestReviewerBondPolicy_DefaultsMatchAcrossParamTypes(t *testing.T) {
	require.Equal(t,
		types.DefaultParams().ReviewerBondPolicy(),
		types.DefaultRepOperationalParams().ReviewerBondPolicy())
}

// ExtractOperationalParams -> ApplyOperationalParams must be lossless, or a
// committee update to one reviewer param would silently reset the others.
func TestReviewerBondPolicy_RoundTripThroughOperational(t *testing.T) {
	p := types.DefaultParams()
	p.MinReviewerBond = math.NewInt(1_250_000_000)
	p.ReviewerDemotionThreshold = math.NewInt(400_000_000)
	p.MinReviewerTrustLevel = "TRUST_LEVEL_CORE"
	p.MinReviewerRepTier = 3
	p.MinReviewerAgeBlocks = 900
	p.ReviewerDemotionCooldown = 86400
	p.ReviewerUnbondCooldown = 172800
	require.NoError(t, p.Validate())

	got := types.DefaultParams().ApplyOperationalParams(p.ExtractOperationalParams())
	require.Equal(t, p.ReviewerBondPolicy(), got.ReviewerBondPolicy())
}

// Every field the write-through projects has to be constrained here, because
// SyncReviewerBondedRoleConfig defaults nothing on the way across.
func TestReviewerBondPolicy_Bounds(t *testing.T) {
	base := types.DefaultParams().ReviewerBondPolicy()
	require.NoError(t, base.Validate())

	testCases := []struct {
		name   string
		mutate func(*types.ReviewerBondPolicy)
		errMsg string
	}{
		{
			// A threshold above the floor would demote every reviewer the
			// moment they bonded the minimum, emptying the roster.
			name:   "demotion threshold above floor",
			mutate: func(rp *types.ReviewerBondPolicy) { rp.DemotionThreshold = rp.MinBond.AddRaw(1) },
			errMsg: "must not exceed min reviewer bond",
		},
		{
			name:   "nil min bond",
			mutate: func(rp *types.ReviewerBondPolicy) { rp.MinBond = math.Int{} },
			errMsg: "min reviewer bond must be non-negative",
		},
		{
			name:   "negative min bond",
			mutate: func(rp *types.ReviewerBondPolicy) { rp.MinBond = math.NewInt(-1) },
			errMsg: "min reviewer bond must be non-negative",
		},
		{
			name:   "nil demotion threshold",
			mutate: func(rp *types.ReviewerBondPolicy) { rp.DemotionThreshold = math.Int{} },
			errMsg: "reviewer demotion threshold must be non-negative",
		},
		{
			// BondRole skips the trust gate entirely on an empty string, so an
			// omitted level would open the one role whose approvals mint DREAM.
			name:   "empty trust level",
			mutate: func(rp *types.ReviewerBondPolicy) { rp.MinTrustLevel = "" },
			errMsg: "must be set",
		},
		{
			name:   "unknown trust level",
			mutate: func(rp *types.ReviewerBondPolicy) { rp.MinTrustLevel = "TRUST_LEVEL_ARCHON" },
			errMsg: "invalid min reviewer trust level",
		},
		{
			name:   "rep tier out of range",
			mutate: func(rp *types.ReviewerBondPolicy) { rp.MinRepTier = 6 },
			errMsg: "must be 0-5",
		},
		{
			name:   "negative age blocks",
			mutate: func(rp *types.ReviewerBondPolicy) { rp.MinAgeBlocks = -1 },
			errMsg: "min reviewer age blocks must be non-negative",
		},
		{
			name:   "negative demotion cooldown",
			mutate: func(rp *types.ReviewerBondPolicy) { rp.DemotionCooldown = -1 },
			errMsg: "reviewer demotion cooldown must be non-negative",
		},
		{
			name:   "negative unbond cooldown",
			mutate: func(rp *types.ReviewerBondPolicy) { rp.UnbondCooldown = -1 },
			errMsg: "reviewer unbond cooldown must be non-negative",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rp := base
			tc.mutate(&rp)
			require.ErrorContains(t, rp.Validate(), tc.errMsg)

			// Both param types run the same policy, so both must reject it.
			require.ErrorContains(t, paramsWithPolicy(rp).Validate(), tc.errMsg)
			require.ErrorContains(t, opParamsWithPolicy(rp).Validate(), tc.errMsg)
		})
	}
}

// Zeros are legitimate values for the four fields that allow them, and must
// survive validation — the write-through relies on that to pass them through
// rather than substituting a default.
func TestReviewerBondPolicy_ZerosAreValid(t *testing.T) {
	rp := types.DefaultParams().ReviewerBondPolicy()
	rp.MinRepTier = 0
	rp.MinAgeBlocks = 0
	rp.DemotionCooldown = 0
	rp.UnbondCooldown = 0
	require.NoError(t, rp.Validate())
	require.NoError(t, paramsWithPolicy(rp).Validate())
	require.NoError(t, opParamsWithPolicy(rp).Validate())

	// A zero floor is permitted too: it opens the roster, but ReserveBond still
	// gates each verdict on the reserve, so verdicts never go unbacked.
	rp.MinBond = math.ZeroInt()
	rp.DemotionThreshold = math.ZeroInt()
	require.NoError(t, rp.Validate())
}

func paramsWithPolicy(rp types.ReviewerBondPolicy) types.Params {
	p := types.DefaultParams()
	p.MinReviewerBond, p.ReviewerDemotionThreshold = rp.MinBond, rp.DemotionThreshold
	p.MinReviewerTrustLevel, p.MinReviewerRepTier = rp.MinTrustLevel, rp.MinRepTier
	p.MinReviewerAgeBlocks = rp.MinAgeBlocks
	p.ReviewerDemotionCooldown, p.ReviewerUnbondCooldown = rp.DemotionCooldown, rp.UnbondCooldown
	return p
}

func opParamsWithPolicy(rp types.ReviewerBondPolicy) types.RepOperationalParams {
	op := types.DefaultRepOperationalParams()
	op.MinReviewerBond, op.ReviewerDemotionThreshold = rp.MinBond, rp.DemotionThreshold
	op.MinReviewerTrustLevel, op.MinReviewerRepTier = rp.MinTrustLevel, rp.MinRepTier
	op.MinReviewerAgeBlocks = rp.MinAgeBlocks
	op.ReviewerDemotionCooldown, op.ReviewerUnbondCooldown = rp.DemotionCooldown, rp.UnbondCooldown
	return op
}
