package types_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// Federation-verifier pay params moved into x/rep when the distribution did.
// They are committee-editable, so every bound has to be enforced on BOTH
// Params and RepOperationalParams -- an operational update that skipped a check
// the governance path enforces would be a way around it.

func TestVerifierPayParams_Defaults(t *testing.T) {
	p := types.DefaultParams()
	require.NoError(t, p.Validate())

	op := types.DefaultRepOperationalParams()
	require.NoError(t, op.Validate())

	// The two constructors must agree, or ExtractOperationalParams and
	// ApplyOperationalParams would round-trip a param to a different value.
	require.Equal(t, p.MaxVerifierRewardPool, op.MaxVerifierRewardPool)
	require.Equal(t, p.VerifierRewardPoolOverflowBurnRatio, op.VerifierRewardPoolOverflowBurnRatio)
	require.Equal(t, p.VerifierRewardEpochBlocks, op.VerifierRewardEpochBlocks)
	require.Equal(t, p.MinVerifierAccuracy, op.MinVerifierAccuracy)
	require.Equal(t, p.VerifierAccuracyWindowEpochs, op.VerifierAccuracyWindowEpochs)
	require.Equal(t, p.MinEpochVerifications, op.MinEpochVerifications)
	require.Equal(t, p.VerifierDreamReward, op.VerifierDreamReward)
	require.Equal(t, p.MaxVerifierDreamMintPerEpoch, op.MaxVerifierDreamMintPerEpoch)
}

func TestVerifierPayParams_RoundTripThroughOperational(t *testing.T) {
	// ExtractOperationalParams -> ApplyOperationalParams must be lossless, or a
	// committee update to one verifier param would silently reset the others.
	p := types.DefaultParams()
	p.MaxVerifierRewardPool = math.NewInt(42_000_000_000)
	p.VerifierRewardEpochBlocks = 777
	p.MinVerifierAccuracy = math.LegacyNewDecWithPrec(65, 2)
	p.MinEpochVerifications = 9
	p.VerifierDreamReward = math.NewInt(3_000_000)
	p.MaxVerifierDreamMintPerEpoch = math.NewInt(31_000_000)
	p.VerifierAccuracyWindowEpochs = 11
	p.VerifierRewardPoolOverflowBurnRatio = math.LegacyNewDecWithPrec(25, 2)
	require.NoError(t, p.Validate())

	got := types.DefaultParams().ApplyOperationalParams(p.ExtractOperationalParams())
	require.Equal(t, p.MaxVerifierRewardPool, got.MaxVerifierRewardPool)
	require.Equal(t, p.VerifierRewardEpochBlocks, got.VerifierRewardEpochBlocks)
	require.Equal(t, p.MinVerifierAccuracy, got.MinVerifierAccuracy)
	require.Equal(t, p.MinEpochVerifications, got.MinEpochVerifications)
	require.Equal(t, p.VerifierDreamReward, got.VerifierDreamReward)
	require.Equal(t, p.MaxVerifierDreamMintPerEpoch, got.MaxVerifierDreamMintPerEpoch)
	require.Equal(t, p.VerifierAccuracyWindowEpochs, got.VerifierAccuracyWindowEpochs)
	require.Equal(t, p.VerifierRewardPoolOverflowBurnRatio, got.VerifierRewardPoolOverflowBurnRatio)
}

func TestVerifierPayParams_Bounds(t *testing.T) {
	cases := []struct {
		name string
		set  func(*types.Params)
	}{
		{"max_verifier_reward_pool nil", func(p *types.Params) {
			p.MaxVerifierRewardPool = math.Int{}
		}},
		{"max_verifier_reward_pool negative", func(p *types.Params) {
			p.MaxVerifierRewardPool = math.NewInt(-1)
		}},
		{"max_verifier_reward_pool above ceiling", func(p *types.Params) {
			// The cap feeds a multiplication and math.Int panics past 256 bits,
			// so an unbounded committee-editable cap is a chain-halt bug.
			p.MaxVerifierRewardPool = types.RoleRewardPoolCeiling().Add(math.OneInt())
		}},
		{"overflow_burn_ratio nil", func(p *types.Params) {
			p.VerifierRewardPoolOverflowBurnRatio = math.LegacyDec{}
		}},
		{"overflow_burn_ratio negative", func(p *types.Params) {
			p.VerifierRewardPoolOverflowBurnRatio = math.LegacyNewDecWithPrec(-1, 1)
		}},
		{"overflow_burn_ratio above one", func(p *types.Params) {
			p.VerifierRewardPoolOverflowBurnRatio = math.LegacyNewDecWithPrec(11, 1)
		}},
		{"reward_epoch_blocks zero", func(p *types.Params) {
			// Zero divides by zero when deriving the epoch number.
			p.VerifierRewardEpochBlocks = 0
		}},
		{"min_verifier_accuracy nil", func(p *types.Params) {
			p.MinVerifierAccuracy = math.LegacyDec{}
		}},
		{"min_verifier_accuracy negative", func(p *types.Params) {
			p.MinVerifierAccuracy = math.LegacyNewDecWithPrec(-1, 1)
		}},
		{"min_verifier_accuracy above one", func(p *types.Params) {
			// An unreachable bar would silently pay nobody, forever.
			p.MinVerifierAccuracy = math.LegacyNewDecWithPrec(11, 1)
		}},
		{"accuracy_window_epochs zero", func(p *types.Params) {
			p.VerifierAccuracyWindowEpochs = 0
		}},
		{"verifier_dream_reward nil", func(p *types.Params) {
			p.VerifierDreamReward = math.Int{}
		}},
		{"verifier_dream_reward negative", func(p *types.Params) {
			p.VerifierDreamReward = math.NewInt(-1)
		}},
		{"max_verifier_dream_mint_per_epoch nil", func(p *types.Params) {
			p.MaxVerifierDreamMintPerEpoch = math.Int{}
		}},
		{"max_verifier_dream_mint_per_epoch negative", func(p *types.Params) {
			p.MaxVerifierDreamMintPerEpoch = math.NewInt(-1)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := types.DefaultParams()
			tc.set(&p)
			require.Error(t, p.Validate(), "Params.Validate must reject")

			op := types.DefaultRepOperationalParams()
			applyToOperational(&op, tc.set)
			require.Error(t, op.Validate(),
				"RepOperationalParams.Validate must reject the same value -- otherwise the committee path bypasses the governance bound")
		})
	}
}

// applyToOperational replays a Params mutation onto a RepOperationalParams by
// routing it through a Params carrier, so the two bound-sets stay tested
// against identical inputs without duplicating every setter.
func applyToOperational(op *types.RepOperationalParams, set func(*types.Params)) {
	carrier := types.DefaultParams()
	set(&carrier)
	op.MaxVerifierRewardPool = carrier.MaxVerifierRewardPool
	op.VerifierRewardPoolOverflowBurnRatio = carrier.VerifierRewardPoolOverflowBurnRatio
	op.VerifierRewardEpochBlocks = carrier.VerifierRewardEpochBlocks
	op.MinVerifierAccuracy = carrier.MinVerifierAccuracy
	op.VerifierAccuracyWindowEpochs = carrier.VerifierAccuracyWindowEpochs
	op.MinEpochVerifications = carrier.MinEpochVerifications
	op.VerifierDreamReward = carrier.VerifierDreamReward
	op.MaxVerifierDreamMintPerEpoch = carrier.MaxVerifierDreamMintPerEpoch
}
