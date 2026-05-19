package types

import (
	"regexp"
)

// service_type registry-key regex (x-service-spec.md §3.2 validation rule).
var serviceTypeKeyRegex = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

// Validate enforces the §3.2 validation rules on a ServiceTypeConfig.
// Run at MsgUpdateServiceTypeConfig time (msg-server) and at genesis
// import. Errors wrap ErrInvalidServiceTypeConfig.
func (c ServiceTypeConfig) Validate() error {
	if !serviceTypeKeyRegex.MatchString(c.ServiceType) {
		return ErrInvalidServiceType.Wrapf("service_type %q must match %s", c.ServiceType, serviceTypeKeyRegex.String())
	}
	if len(c.Description) > 512 {
		return ErrInvalidServiceTypeConfig.Wrapf("description length %d > 512", len(c.Description))
	}

	// min_bond must be a positive uspark amount.
	if c.MinBond.Denom != BondDenom {
		return ErrInvalidServiceTypeConfig.Wrapf("min_bond.denom must be %s, got %s", BondDenom, c.MinBond.Denom)
	}
	if c.MinBond.Amount.IsNil() || !c.MinBond.Amount.IsPositive() {
		return ErrInvalidServiceTypeConfig.Wrap("min_bond.amount must be > 0")
	}

	// Bps in (0, 10000].
	if c.UnilateralSlashCapBps == 0 || c.UnilateralSlashCapBps > 10000 {
		return ErrInvalidServiceTypeConfig.Wrapf("unilateral_slash_cap_bps must be in (0, 10000], got %d", c.UnilateralSlashCapBps)
	}
	if c.Tier1AggregateCapBps == 0 || c.Tier1AggregateCapBps > 10000 {
		return ErrInvalidServiceTypeConfig.Wrapf("tier1_aggregate_cap_bps must be in (0, 10000], got %d", c.Tier1AggregateCapBps)
	}

	// Aggregate cap ≥ unilateral cap (otherwise one tier-1 slash could
	// exceed the aggregate cap immediately).
	if c.Tier1AggregateCapBps < c.UnilateralSlashCapBps {
		return ErrInvalidServiceTypeConfig.Wrapf(
			"tier1_aggregate_cap_bps (%d) < unilateral_slash_cap_bps (%d)",
			c.Tier1AggregateCapBps, c.UnilateralSlashCapBps,
		)
	}

	// All *_blocks fields must be > 0 (zero would break their respective
	// timing invariants).
	type blocksField struct {
		name  string
		value int64
	}
	for _, f := range []blocksField{
		{"unbonding_period_blocks", c.UnbondingPeriodBlocks},
		{"tier1_window_blocks", c.Tier1WindowBlocks},
		{"tier1_cooldown_blocks", c.Tier1CooldownBlocks},
		{"underfunded_grace_blocks", c.UnderfundedGraceBlocks},
	} {
		if f.value <= 0 {
			return ErrInvalidServiceTypeConfig.Wrapf("%s must be > 0, got %d", f.name, f.value)
		}
	}

	// challenge_default_slash_bps cross-field invariant: must be <=
	// unilateral_slash_cap_bps. Validated in both directions at update
	// time (Phase 0 federation→service migration) so:
	//   - a proposal raising challenge_default_slash_bps above the cap
	//     fails up-front rather than landing an invalid state;
	//   - a proposal lowering the cap below the existing default also
	//     fails — gov UX guidance is to either bundle both fields in
	//     one update or split into two ordered proposals (default first,
	//     cap second).
	// Zero is allowed: callers of OpenSystemReport that explicitly pass
	// slashBps don't need a configured default, and ServiceTypes never
	// touched by OpenSystemReport (e.g. non-federation operators) can
	// leave it at zero.
	if c.ChallengeDefaultSlashBps > 10000 {
		return ErrInvalidServiceTypeConfig.Wrapf(
			"challenge_default_slash_bps must be in [0, 10000], got %d",
			c.ChallengeDefaultSlashBps,
		)
	}
	if c.ChallengeDefaultSlashBps > c.UnilateralSlashCapBps {
		return ErrInvalidServiceTypeConfig.Wrapf(
			"challenge_default_slash_bps (%d) must be <= unilateral_slash_cap_bps (%d) — see the migration spec for the gov-UX rationale",
			c.ChallengeDefaultSlashBps, c.UnilateralSlashCapBps,
		)
	}

	// ReportTimeoutAction: anything outside the enum range is rejected.
	if _, ok := ReportTimeoutAction_name[int32(c.ReportTimeoutAction)]; !ok {
		return ErrInvalidServiceTypeConfig.Wrapf(
			"report_timeout_action %d is not a valid enum value",
			c.ReportTimeoutAction,
		)
	}

	return nil
}
