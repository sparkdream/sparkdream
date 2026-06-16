package types_test

import (
	"testing"

	"cosmossdk.io/math"

	"sparkdream/x/forum/types"
)

func TestDefaultParams_Validates(t *testing.T) {
	p := types.DefaultParams()
	if err := p.Validate(); err != nil {
		t.Fatalf("DefaultParams should validate, got %v", err)
	}
}

func TestParams_Validate(t *testing.T) {
	good := types.DefaultParams()

	cases := []struct {
		name    string
		mutate  func(p *types.Params)
		wantErr bool
	}{
		{"default", func(*types.Params) {}, false},
		{"zero ephemeral ttl", func(p *types.Params) { p.EphemeralTtl = 0 }, true},
		{"negative ephemeral ttl", func(p *types.Params) { p.EphemeralTtl = -1 }, true},
		{"negative cost_per_byte_amount", func(p *types.Params) {
			p.CostPerByteAmount = math.NewInt(-1)
		}, true},
		// Moderation rate caps: 0 = unset (ok); above the ceiling = reject.
		{"hides cap at ceiling ok", func(p *types.Params) { p.MaxHidesPerEpoch = types.ModerationEpochCapCeiling }, false},
		{"hides cap over ceiling", func(p *types.Params) { p.MaxHidesPerEpoch = types.ModerationEpochCapCeiling + 1 }, true},
		{"locks cap over ceiling", func(p *types.Params) { p.MaxSentinelLocksPerEpoch = types.ModerationEpochCapCeiling + 1 }, true},
		{"moves cap over ceiling", func(p *types.Params) { p.MaxSentinelMovesPerEpoch = types.ModerationEpochCapCeiling + 1 }, true},
		// Slash amount: positive and <= the base bond.
		{"slash non-positive", func(p *types.Params) { p.SentinelSlashAmount = "0" }, true},
		{"slash unparseable", func(p *types.Params) { p.SentinelSlashAmount = "abc" }, true},
		{"slash equals min bond ok", func(p *types.Params) { p.SentinelSlashAmount = p.MinSentinelBond }, false},
		{"slash exceeds min bond", func(p *types.Params) {
			p.SentinelSlashAmount = p.MinSentinelBondOrDefault().AddRaw(1).String()
		}, true},
		// Lock backing: positive and >= the derived lock bond (4 × 500 = 2000 DREAM).
		{"backing below lock bond", func(p *types.Params) { p.LockBackingAmount = "1999000000" }, true},
		{"backing equals lock bond ok", func(p *types.Params) {
			p.LockBackingAmount = p.LockMinBondOrDefault().String()
		}, false},
		// Lock min rep tier: within [min_sentinel_rep_tier, 5].
		{"lock tier over 5", func(p *types.Params) { p.LockMinRepTier = 6 }, true},
		{"lock tier below base tier", func(p *types.Params) {
			p.MinSentinelRepTier = 3
			p.LockMinRepTier = 2
		}, true},
		{"lock tier 5 ok", func(p *types.Params) { p.LockMinRepTier = 5 }, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := good
			tc.mutate(&p)
			err := p.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestForumOperationalParams_Validate(t *testing.T) {
	good := types.DefaultForumOperationalParams()

	cases := []struct {
		name    string
		mutate  func(p *types.ForumOperationalParams)
		wantErr bool
	}{
		{"default", func(*types.ForumOperationalParams) {}, false},
		{"zero ephemeral ttl", func(p *types.ForumOperationalParams) { p.EphemeralTtl = 0 }, true},
		{"negative cost_per_byte_amount", func(p *types.ForumOperationalParams) {
			p.CostPerByteAmount = math.NewInt(-1)
		}, true},
		{"bounty cancel fee over 100", func(p *types.ForumOperationalParams) { p.BountyCancellationFeePercent = 101 }, true},
		{"negative conviction renewal threshold", func(p *types.ForumOperationalParams) {
			p.ConvictionRenewalThreshold = math.LegacyNewDec(-1)
		}, true},
		{"negative conviction renewal period", func(p *types.ForumOperationalParams) {
			p.ConvictionRenewalPeriod = -1
		}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := good
			tc.mutate(&p)
			err := p.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestApplyAndExtractOperationalParams_RoundTrip(t *testing.T) {
	p := types.DefaultParams()
	op := p.ExtractOperationalParams()
	p2 := p.ApplyOperationalParams(op)

	if p.EphemeralTtl != p2.EphemeralTtl {
		t.Errorf("EphemeralTtl changed: %d vs %d", p.EphemeralTtl, p2.EphemeralTtl)
	}
	if !p.SpamTaxAmount.Equal(p2.SpamTaxAmount) {
		t.Errorf("SpamTaxAmount changed: %s vs %s", p.SpamTaxAmount, p2.SpamTaxAmount)
	}
	if p.MaxContentSize != p2.MaxContentSize {
		t.Errorf("MaxContentSize changed: %d vs %d", p.MaxContentSize, p2.MaxContentSize)
	}
}

func TestApplyOperationalParams_PreservesPauseFlags(t *testing.T) {
	p := types.DefaultParams()
	p.ForumPaused = true
	p.ModerationPaused = true
	p.AppealsPaused = true

	op := types.DefaultForumOperationalParams()
	op.EphemeralTtl = 7777

	p2 := p.ApplyOperationalParams(op)

	if !p2.ForumPaused || !p2.ModerationPaused || !p2.AppealsPaused {
		t.Errorf("pause flags not preserved: forum=%v moderation=%v appeals=%v",
			p2.ForumPaused, p2.ModerationPaused, p2.AppealsPaused)
	}
	if p2.EphemeralTtl != 7777 {
		t.Errorf("EphemeralTtl not applied: %d", p2.EphemeralTtl)
	}
}

func TestModerationParamAccessors_ZeroMeansDefault(t *testing.T) {
	// An all-zero/empty Params (e.g. the state an existing chain sees right
	// after the upgrade adds these fields) must fall back to the historical
	// defaults — this is what makes the change migration-free.
	var p types.Params
	if got := p.MaxHidesPerEpochOrDefault(); got != types.DefaultMaxHidesPerEpoch {
		t.Errorf("hides default: got %d want %d", got, types.DefaultMaxHidesPerEpoch)
	}
	if got := p.MaxSentinelLocksPerEpochOrDefault(); got != types.DefaultMaxSentinelLocksPerEpoch {
		t.Errorf("locks default: got %d want %d", got, types.DefaultMaxSentinelLocksPerEpoch)
	}
	if got := p.MaxSentinelMovesPerEpochOrDefault(); got != types.DefaultMaxSentinelMovesPerEpoch {
		t.Errorf("moves default: got %d want %d", got, types.DefaultMaxSentinelMovesPerEpoch)
	}
	if got := p.SentinelSlashAmountOrDefault(); !got.Equal(math.NewInt(types.DefaultSentinelSlashAmount)) {
		t.Errorf("slash default: got %s", got)
	}
	if got := p.LockMinRepTierOrDefault(); got != types.DefaultMinRepTierThreadLock {
		t.Errorf("lock tier default: got %d want %d", got, types.DefaultMinRepTierThreadLock)
	}
	// Derived lock bond = multiplier(4) × min_sentinel_bond(500 DREAM) = 2000 DREAM.
	wantLockBond := types.DefaultMinSentinelBond.Mul(math.NewIntFromUint64(types.DefaultLockBondMultiplier))
	if got := p.LockMinBondOrDefault(); !got.Equal(wantLockBond) {
		t.Errorf("lock bond default: got %s want %s", got, wantLockBond)
	}
	if got := p.LockBackingAmountOrDefault(); !got.Equal(types.DefaultLockBackingAmount) {
		t.Errorf("lock backing default: got %s", got)
	}
}

func TestLockMinBond_TracksBaseBondAndMultiplier(t *testing.T) {
	// The lock bond is derived from the base bond, so raising either the base
	// bond or the multiplier raises the lock floor — no separate hardcoded
	// value to drift out of sync.
	p := types.DefaultParams()
	p.MinSentinelBond = "1000000000" // 1000 DREAM base
	p.LockBondMultiplier = 3
	want := math.NewInt(3_000_000_000) // 3 × 1000 DREAM
	if got := p.LockMinBondOrDefault(); !got.Equal(want) {
		t.Errorf("derived lock bond: got %s want %s", got, want)
	}
}

func TestForumOperationalParams_ModerationCapBounds(t *testing.T) {
	good := types.DefaultForumOperationalParams()
	cases := []struct {
		name    string
		mutate  func(p *types.ForumOperationalParams)
		wantErr bool
	}{
		{"hides over ceiling", func(p *types.ForumOperationalParams) { p.MaxHidesPerEpoch = types.ModerationEpochCapCeiling + 1 }, true},
		{"locks over ceiling", func(p *types.ForumOperationalParams) {
			p.MaxSentinelLocksPerEpoch = types.ModerationEpochCapCeiling + 1
		}, true},
		{"moves over ceiling", func(p *types.ForumOperationalParams) {
			p.MaxSentinelMovesPerEpoch = types.ModerationEpochCapCeiling + 1
		}, true},
		{"slash non-positive", func(p *types.ForumOperationalParams) { p.SentinelSlashAmount = "0" }, true},
		{"caps within ceiling ok", func(p *types.ForumOperationalParams) {
			p.MaxHidesPerEpoch = 3
			p.MaxSentinelLocksPerEpoch = 1
			p.MaxSentinelMovesPerEpoch = 2
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := good
			tc.mutate(&p)
			err := p.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestApplyOperationalParams_LeavesLockFloorsIntact(t *testing.T) {
	// Lock floors are governance-only; an ops-committee operational update must
	// not touch lock_bond_multiplier / lock_backing_amount / lock_min_rep_tier.
	p := types.DefaultParams()
	p.LockBondMultiplier = 7
	p.LockBackingAmount = "50000000000"
	p.LockMinRepTier = 5

	op := p.ExtractOperationalParams()
	op.MaxSentinelLocksPerEpoch = 2 // an ops-tunable change
	p2 := p.ApplyOperationalParams(op)

	if p2.LockBondMultiplier != 7 || p2.LockBackingAmount != "50000000000" || p2.LockMinRepTier != 5 {
		t.Errorf("lock floors mutated by operational update: mult=%d backing=%s tier=%d",
			p2.LockBondMultiplier, p2.LockBackingAmount, p2.LockMinRepTier)
	}
	if p2.MaxSentinelLocksPerEpoch != 2 {
		t.Errorf("operational cap not applied: %d", p2.MaxSentinelLocksPerEpoch)
	}
}

func TestDefaultValueHelpers(t *testing.T) {
	if types.DefaultMaxContentSizeValue() != types.DefaultMaxContentSize {
		t.Error("DefaultMaxContentSizeValue mismatch")
	}
	if types.DefaultDailyPostLimitValue() != types.DefaultDailyPostLimit {
		t.Error("DefaultDailyPostLimitValue mismatch")
	}
	if types.DefaultMaxReplyDepthValue() != types.DefaultMaxReplyDepth {
		t.Error("DefaultMaxReplyDepthValue mismatch")
	}
	if types.DefaultEphemeralTTLValue() != types.DefaultEphemeralTTL {
		t.Error("DefaultEphemeralTTLValue mismatch")
	}
	if types.DefaultEditGracePeriodValue() != types.DefaultEditGracePeriod {
		t.Error("DefaultEditGracePeriodValue mismatch")
	}
	if types.DefaultEditMaxWindowValue() != types.DefaultEditMaxWindow {
		t.Error("DefaultEditMaxWindowValue mismatch")
	}
}
