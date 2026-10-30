package types_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
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
// min_verifier_accuracy and operator_reward_share.
func TestParamsValidate_LegacyDecBounds(t *testing.T) {
	cases := []struct {
		name string
		set  func(*types.Params)
	}{
		{"operator_reward_inflation_share < 0", func(p *types.Params) {
			p.OperatorRewardInflationShare = math.LegacyNewDecWithPrec(-1, 1)
		}},
		{"operator_reward_inflation_share > 1", func(p *types.Params) {
			p.OperatorRewardInflationShare = math.LegacyNewDecWithPrec(11, 1)
		}},
		{"max_unverified_rate > 1", func(p *types.Params) {
			p.MaxUnverifiedRate = math.LegacyNewDecWithPrec(11, 1)
		}},
		{"operator_reward_pool_overflow_burn_ratio < 0", func(p *types.Params) {
			p.OperatorRewardPoolOverflowBurnRatio = math.LegacyNewDecWithPrec(-1, 1)
		}},
		{"operator_reward_pool_overflow_burn_ratio > 1", func(p *types.Params) {
			p.OperatorRewardPoolOverflowBurnRatio = math.LegacyNewDecWithPrec(11, 1)
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

// TestOperatorRewardParams covers the bridge-operator compensation params that
// the Dec-bounds and nil-Int tables above do not reach: the epoch cadence
// (a divisor) and the pool cap.
func TestOperatorRewardParams(t *testing.T) {
	t.Run("defaults validate", func(t *testing.T) {
		require.NoError(t, types.DefaultParams().Validate())
	})

	t.Run("epoch_blocks zero rejected", func(t *testing.T) {
		// Zero divides by zero when deriving the epoch number.
		p := types.DefaultParams()
		p.OperatorRewardEpochBlocks = 0
		require.ErrorContains(t, p.Validate(), "operator_reward_epoch_blocks")
	})

	t.Run("max_pool nil rejected", func(t *testing.T) {
		p := types.DefaultParams()
		p.MaxOperatorRewardPool = math.Int{}
		require.ErrorContains(t, p.Validate(), "max_operator_reward_pool")
	})

	t.Run("max_pool negative rejected", func(t *testing.T) {
		p := types.DefaultParams()
		p.MaxOperatorRewardPool = math.NewInt(-1)
		require.ErrorContains(t, p.Validate(), "max_operator_reward_pool")
	})

	t.Run("inflation_share zero is legal and disables funding", func(t *testing.T) {
		// Zero is meaningful, not a misconfiguration: it turns off the
		// automatic community-pool draw without disabling distribution of
		// whatever the pool already holds.
		p := types.DefaultParams()
		p.OperatorRewardInflationShare = math.LegacyZeroDec()
		require.NoError(t, p.Validate())
	})

	t.Run("min_epoch_verified_submissions zero rejected", func(t *testing.T) {
		// Zero would pay an operator who got nothing verified all epoch,
		// which is the whole condition the compensation rests on.
		p := types.DefaultParams()
		p.MinEpochVerifiedSubmissions = 0
		require.ErrorContains(t, p.Validate(), "min_epoch_verified_submissions")
	})
}

// FED-20: the remaining spec §4.13 ceilings and the DREAM amount floors are
// enforced by Params.Validate on every build.
func TestParamsValidate_SpecCeilings(t *testing.T) {
	cases := map[string]func(p *types.Params){
		"challenge_ttl":          func(p *types.Params) { p.ChallengeTtl = 31 * 24 * time.Hour },
		"verifier_unbond":        func(p *types.Params) { p.VerifierUnbondCooldown = 91 * 24 * time.Hour },
		"arbiter_resolution":     func(p *types.Params) { p.ArbiterResolutionWindow = 8 * 24 * time.Hour },
		"bridge_inactivity":      func(p *types.Params) { p.BridgeInactivityThreshold = 10_001 },
		"min_verifier_bond_high": func(p *types.Params) { p.MinVerifierBond = math.NewInt(10_001_000_000) },
		"min_verifier_bond_low": func(p *types.Params) {
			p.MinVerifierBond = math.NewInt(99_000_000)
			p.VerifierRecoveryThreshold = math.NewInt(50_000_000)
			p.VerifierSlashAmount = math.NewInt(10_000_000)
		},
		"slash_low":      func(p *types.Params) { p.VerifierSlashAmount = math.NewInt(9_000_000) },
		"challenge_fee":  func(p *types.Params) { p.ChallengeFeeAmount = math.NewInt(5_001_000_000) },
		"escalation_fee": func(p *types.Params) { p.EscalationFeeAmount = math.NewInt(1_001_000_000) },
		"recovery_floor": func(p *types.Params) { p.VerifierRecoveryThreshold = math.NewInt(49_000_000) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := types.DefaultParams()
			require.NoError(t, p.Validate())
			mutate(&p)
			require.Error(t, p.Validate())
		})
	}
}

// loadNetworkParams decodes the federation params of a network genesis file.
func loadNetworkParams(t *testing.T, network string) types.Params {
	t.Helper()
	bz, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "config", "network", network, "genesis.json"))
	require.NoError(t, err)
	var gen struct {
		AppState struct {
			Federation struct {
				Params json.RawMessage `json:"params"`
			} `json:"federation"`
		} `json:"app_state"`
	}
	require.NoError(t, json.Unmarshal(bz, &gen))
	var p types.Params
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	require.NoError(t, cdc.UnmarshalJSON(gen.AppState.Federation.Params, &p))
	return p
}

// TestParamsValidate_NetworkFloors (FED-20): mainnet and testnet enforce the
// spec §4.13 floors; devnet enforces its own lower floors; the test build
// none. Every network genesis passes its own floors, and each spec floor
// refuses a value just below it.
func TestParamsValidate_NetworkFloors(t *testing.T) {
	for network, floors := range map[string]types.ParamFloors{
		"mainnet": types.SpecParamFloors,
		"testnet": types.SpecParamFloors,
		"devnet":  types.DevnetParamFloors,
	} {
		p := loadNetworkParams(t, network)
		require.NoError(t, p.ValidateWithFloors(floors), network)
	}
	// Devnet's short windows are below the spec floors, so it needs its own.
	require.Error(t, loadNetworkParams(t, "devnet").ValidateWithFloors(types.SpecParamFloors))

	f := types.SpecParamFloors
	below := map[string]func(p *types.Params){
		"content_ttl":                     func(p *types.Params) { p.ContentTtl = f.RetentionTTL - time.Second },
		"attestation_ttl":                 func(p *types.Params) { p.AttestationTtl = f.RetentionTTL - time.Second },
		"challenge_ttl":                   func(p *types.Params) { p.ChallengeTtl = f.ChallengeTTL - time.Second },
		"rate_limit_window":               func(p *types.Params) { p.RateLimitWindow = f.RateLimitWindow - time.Second },
		"verification_window":             func(p *types.Params) { p.VerificationWindow = f.VerificationWindow - time.Second },
		"challenge_window":                func(p *types.Params) { p.ChallengeWindow = f.ChallengeWindow - time.Second },
		"challenge_jury_deadline":         func(p *types.Params) { p.ChallengeJuryDeadline = f.ChallengeJuryDeadline - time.Second },
		"verifier_demotion_cooldown":      func(p *types.Params) { p.VerifierDemotionCooldown = f.VerifierDemotionCooldown - time.Second },
		"verifier_overturn_base_cooldown": func(p *types.Params) { p.VerifierOverturnBaseCooldown = f.VerifierOverturnBaseCooldown - time.Second },
		"arbiter_resolution_window":       func(p *types.Params) { p.ArbiterResolutionWindow = f.ArbiterResolutionWindow - time.Second },
		"arbiter_escalation_window":       func(p *types.Params) { p.ArbiterEscalationWindow = f.ArbiterEscalationWindow - time.Second },
		"challenge_cooldown":              func(p *types.Params) { p.ChallengeCooldown = f.ChallengeCooldown - time.Second },
		"challenge_fee_amount":            func(p *types.Params) { p.ChallengeFeeAmount = math.NewInt(f.ChallengeFeeSpark*1_000_000 - 1) },
		"escalation_fee_amount":           func(p *types.Params) { p.EscalationFeeAmount = math.NewInt(f.EscalationFeeSpark*1_000_000 - 1) },
		"bridge_inactivity_threshold":     func(p *types.Params) { p.BridgeInactivityThreshold = f.BridgeInactivityThreshold - 1 },
	}
	for name, mutate := range below {
		t.Run(name, func(t *testing.T) {
			p := loadNetworkParams(t, "mainnet")
			mutate(&p)
			require.ErrorContains(t, p.ValidateWithFloors(types.SpecParamFloors), name)
			// The test build keeps accepting it (e2e windows are seconds).
			require.NoError(t, p.ValidateWithFloors(types.TestParamFloors))
		})
	}

	// Whatever the build, its own defaults pass its own floors.
	require.NoError(t, types.DefaultParams().Validate())
}
