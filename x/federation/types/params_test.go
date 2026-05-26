package types_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/types"
)

// TestDefaultParamsValidate confirms DefaultParams() produces a
// Params blob that passes Validate(). This is a regression guard:
// every time the params struct grows a new field, DefaultParams must
// populate it AND Validate must accept the default. A failure here
// usually means a new field was added to one but not the other.
func TestDefaultParamsValidate(t *testing.T) {
	require.NoError(t, types.DefaultParams().Validate())
}

// TestParamsValidate_RelationalConstraints exercises the two
// non-negotiable relational checks: verifier_recovery_threshold <
// min_verifier_bond, and verifier_slash_amount <= min_verifier_bond.
// Either inversion produces ill-defined runtime behavior, so the
// handler must reject the proposal before it lands.
func TestParamsValidate_RelationalConstraints(t *testing.T) {
	t.Run("recovery_threshold >= min_bond rejected", func(t *testing.T) {
		p := types.DefaultParams()
		p.VerifierRecoveryThreshold = p.MinVerifierBond // equal — also rejected
		err := p.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "verifier_recovery_threshold")
		require.Contains(t, err.Error(), "min_verifier_bond")
	})
	t.Run("recovery_threshold > min_bond rejected", func(t *testing.T) {
		p := types.DefaultParams()
		p.VerifierRecoveryThreshold = p.MinVerifierBond.Add(math.OneInt())
		require.Error(t, p.Validate())
	})
	t.Run("slash_amount > min_bond rejected", func(t *testing.T) {
		p := types.DefaultParams()
		p.VerifierSlashAmount = p.MinVerifierBond.Add(math.OneInt())
		err := p.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "verifier_slash_amount")
	})
	t.Run("slash_amount == min_bond accepted", func(t *testing.T) {
		// Edge case: slashing the full bond drains the verifier in
		// one overturn but isn't ill-defined. Explicitly allowed.
		p := types.DefaultParams()
		p.VerifierSlashAmount = p.MinVerifierBond
		require.NoError(t, p.Validate())
	})
}

// TestParamsValidate_LegacyDecBounds locks the [0,1] ranges on
// trust_discount_rate, min_verifier_accuracy, and operator_reward_share.
func TestParamsValidate_LegacyDecBounds(t *testing.T) {
	cases := []struct {
		name string
		set  func(*types.Params)
	}{
		{"trust_discount_rate < 0", func(p *types.Params) {
			p.TrustDiscountRate = math.LegacyNewDecWithPrec(-1, 1)
		}},
		{"trust_discount_rate > 1", func(p *types.Params) {
			p.TrustDiscountRate = math.LegacyNewDecWithPrec(11, 1)
		}},
		{"min_verifier_accuracy > 1", func(p *types.Params) {
			p.MinVerifierAccuracy = math.LegacyNewDecWithPrec(11, 1)
		}},
		{"operator_reward_share < 0", func(p *types.Params) {
			p.OperatorRewardShare = math.LegacyNewDecWithPrec(-1, 1)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := types.DefaultParams()
			tc.set(&p)
			require.Error(t, p.Validate())
		})
	}
}

// TestParamsValidate_NilIntFields rejects unset (nil) math.Int fields.
// Proto-decoding a Params blob without these can leave them nil; the
// validator should catch that rather than panic at runtime.
func TestParamsValidate_NilIntFields(t *testing.T) {
	cases := []func(*types.Params){
		func(p *types.Params) { p.MinVerifierBond = math.Int{} },
		func(p *types.Params) { p.VerifierRecoveryThreshold = math.Int{} },
		func(p *types.Params) { p.VerifierSlashAmount = math.Int{} },
		func(p *types.Params) { p.VerifierDreamReward = math.Int{} },
		func(p *types.Params) { p.MaxVerifierDreamMintPerEpoch = math.Int{} },
		func(p *types.Params) { p.ChallengeFeeAmount = math.Int{} },
		func(p *types.Params) { p.EscalationFeeAmount = math.Int{} },
	}
	for i, fn := range cases {
		p := types.DefaultParams()
		fn(&p)
		err := p.Validate()
		require.Errorf(t, err, "case %d should reject nil int field", i)
		require.Contains(t, err.Error(), "must be set")
	}
}

// TestParamsValidate_DurationPositivity rejects zero-or-negative
// durations on the per-mechanism timers.
func TestParamsValidate_DurationPositivity(t *testing.T) {
	cases := []func(*types.Params){
		func(p *types.Params) { p.ContentTtl = 0 },
		func(p *types.Params) { p.ChallengeJuryDeadline = -time.Second },
		func(p *types.Params) { p.ArbiterResolutionWindow = 0 },
		func(p *types.Params) { p.RateLimitWindow = 0 },
	}
	for i, fn := range cases {
		p := types.DefaultParams()
		fn(&p)
		require.Errorf(t, p.Validate(), "case %d should reject non-positive duration", i)
	}
}

// TestParamsValidate_TrustLevelBounds enforces [1, 4] on
// min_verifier_trust_level (0=NEWCOMER is rejected — verifiers must
// have demonstrated some standing).
func TestParamsValidate_TrustLevelBounds(t *testing.T) {
	t.Run("0 rejected", func(t *testing.T) {
		p := types.DefaultParams()
		p.MinVerifierTrustLevel = 0
		require.Error(t, p.Validate())
	})
	t.Run("5 rejected", func(t *testing.T) {
		p := types.DefaultParams()
		p.MinVerifierTrustLevel = 5
		require.Error(t, p.Validate())
	})
}

// TestParamsValidate_QuorumMin rejects arbiter_quorum < 2 (1 means a
// single arbiter can auto-resolve, defeating the purpose of consensus).
func TestParamsValidate_QuorumMin(t *testing.T) {
	p := types.DefaultParams()
	p.ArbiterQuorum = 1
	err := p.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "arbiter_quorum")
}
