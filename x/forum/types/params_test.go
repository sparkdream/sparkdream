package types_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

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
		{"negative cost_per_byte", func(p *types.Params) {
			p.CostPerByte = sdk.Coin{Denom: types.DefaultFeeDenom, Amount: math.NewInt(-1)}
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

func TestForumOperationalParams_Validate(t *testing.T) {
	good := types.DefaultForumOperationalParams()

	cases := []struct {
		name    string
		mutate  func(p *types.ForumOperationalParams)
		wantErr bool
	}{
		{"default", func(*types.ForumOperationalParams) {}, false},
		{"zero ephemeral ttl", func(p *types.ForumOperationalParams) { p.EphemeralTtl = 0 }, true},
		{"negative cost_per_byte", func(p *types.ForumOperationalParams) {
			p.CostPerByte = sdk.Coin{Denom: types.DefaultFeeDenom, Amount: math.NewInt(-1)}
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
	if !p.SpamTax.IsEqual(p2.SpamTax) {
		t.Errorf("SpamTax changed: %s vs %s", p.SpamTax, p2.SpamTax)
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
