package types

import (
	"cosmossdk.io/math"
)

// Default param values. See docs/x-service-spec.md §4.2 for the full table
// and validation rules. Block-denominated durations assume ~5s block time
// (typical for this chain); operators retuning these should be aware of
// the validation invariants (e.g. unbonding ≥ contest window).
const (
	// ~14 days at 5s blocks (14 * 24 * 60 * 60 / 5 = 241,920).
	DefaultUnbondingPeriodBlocks int64 = 241_920
	// ~24 hours at 5s blocks (24 * 60 * 60 / 5 = 17,280).
	DefaultReportContestWindowBlocks int64 = 17_280
	// ~30 days at 5s blocks (30 * 24 * 60 * 60 / 5 = 518,400).
	DefaultMaxPendingBlocks int64 = 518_400
	// ~60 days at 5s blocks.
	DefaultMaxEscalatedBlocks int64 = 1_036_800
	// ~30 days at 5s blocks.
	DefaultReportRefileCooldownBlocks int64 = 518_400
	// ~7 days at 5s blocks.
	DefaultUnderfundedGraceBlocks int64 = 120_960
	// ~7 days at 5s blocks.
	DefaultTier1CooldownBlocks int64 = 120_960
	// ~90 days at 5s blocks.
	DefaultTier1WindowBlocks int64 = 1_555_200
	// ~30 days at 5s blocks.
	DefaultReporterRateLimitWindowBlocks int64 = 518_400
	// ~1 day at 5s blocks (24 * 60 * 60 / 5 = 17,280). Window over
	// which OpenSystemReport's per-caller cap accumulates.
	DefaultRateLimitWindowBlocks int64 = 17_280

	DefaultUnilateralSlashCapBps                     uint32 = 500  // 5%
	DefaultTier1AggregateCapBps                      uint32 = 1500 // 15%
	DefaultEndBlockerSweepLimit                      uint32 = 100
	DefaultMaxMetadataBytes                          uint32 = 4096
	DefaultMaxReasonBytes                            uint32 = 512
	DefaultMaxActiveOperatorsPerAddress              uint32 = 16
	DefaultMaxReportsPerReporterPerOperatorPerWindow uint32 = 3
	DefaultPaginationLimit                           uint32 = 100
	DefaultMaxPaginationLimit                        uint32 = 1000
	// Phase 0 federation→service migration: per-caller cap on
	// OpenSystemReport filings within RateLimitWindowBlocks. 50 is a
	// starting guess; tune from observed federation challenge volume
	// post-launch.
	DefaultMaxSystemReportsPerCallerPerWindow uint32 = 50

	// Default trust-level gate for filing reports. See §16 launch note —
	// genesis params SHOULD lower to PROVISIONAL for the first season.
	DefaultMinReporterTrustLevel = "TRUST_LEVEL_ESTABLISHED"
)

// DefaultReportDepositAmount is the SPARK deposit required to file a
// report (10 SPARK in uspark = 10_000_000 uspark).
var DefaultReportDepositAmount = math.NewInt(10_000_000)

// DefaultReputationGrantPerBondBlock is the §6.6 accrual rate. Tuned so
// that ~1 SPARK bonded for ~1 day of blocks yields ~1 reputation point
// at unbond-claim time; concrete value to be calibrated during sim.
var DefaultReputationGrantPerBondBlock = math.LegacyNewDecWithPrec(1, 12) // 1e-12

// NewParams creates a new Params instance from the supplied fields.
func NewParams() Params {
	return DefaultParams()
}

// DefaultParams returns a fully-populated default set of parameters. All
// nullable=false fields (math.Int, LegacyDec) MUST be initialized here
// to avoid nil internals at marshal/unmarshal round-trips. Bond-denom-
// valued amounts (ReportDepositAmount) are bare math.Int values; the
// keeper wraps them into sdk.Coin via the identity keeper's
// BondDenom(ctx) at the point of use.
func DefaultParams() Params {
	return Params{
		DefaultUnbondingPeriodBlocks:              DefaultUnbondingPeriodBlocks,
		DefaultUnilateralSlashCapBps:              DefaultUnilateralSlashCapBps,
		DefaultTier1WindowBlocks:                  DefaultTier1WindowBlocks,
		DefaultTier1AggregateCapBps:               DefaultTier1AggregateCapBps,
		DefaultTier1CooldownBlocks:                DefaultTier1CooldownBlocks,
		DefaultUnderfundedGraceBlocks:             DefaultUnderfundedGraceBlocks,
		ReportContestWindowBlocks:                 DefaultReportContestWindowBlocks,
		MaxPendingBlocks:                          DefaultMaxPendingBlocks,
		MaxEscalatedBlocks:                        DefaultMaxEscalatedBlocks,
		ReportRefileCooldownBlocks:                DefaultReportRefileCooldownBlocks,
		ReportDepositAmount:                       DefaultReportDepositAmount,
		MinReporterTrustLevel:                     DefaultMinReporterTrustLevel,
		MaxReportsPerReporterPerOperatorPerWindow: DefaultMaxReportsPerReporterPerOperatorPerWindow,
		ReporterRateLimitWindowBlocks:             DefaultReporterRateLimitWindowBlocks,
		EndblockerSweepLimit:                      DefaultEndBlockerSweepLimit,
		MaxMetadataBytes:                          DefaultMaxMetadataBytes,
		MaxReasonBytes:                            DefaultMaxReasonBytes,
		MaxActiveOperatorsPerAddress:              DefaultMaxActiveOperatorsPerAddress,
		ReputationGrantPerBondBlock:               DefaultReputationGrantPerBondBlock,
		DefaultPaginationLimit:                    DefaultPaginationLimit,
		MaxPaginationLimit:                        DefaultMaxPaginationLimit,
		MaxSystemReportsPerCallerPerWindow:        DefaultMaxSystemReportsPerCallerPerWindow,
		RateLimitWindowBlocks:                     DefaultRateLimitWindowBlocks,
	}
}

// Validate validates the set of params per x-service-spec.md §4.2
// validation rules. Returns nil iff all constraints hold; otherwise
// returns a wrapped ErrInvalidParams.
func (p Params) Validate() error {
	// All *_blocks params > 0.
	blocksFields := map[string]int64{
		"default_unbonding_period_blocks":   p.DefaultUnbondingPeriodBlocks,
		"default_tier1_window_blocks":       p.DefaultTier1WindowBlocks,
		"default_tier1_cooldown_blocks":     p.DefaultTier1CooldownBlocks,
		"default_underfunded_grace_blocks":  p.DefaultUnderfundedGraceBlocks,
		"report_contest_window_blocks":      p.ReportContestWindowBlocks,
		"max_pending_blocks":                p.MaxPendingBlocks,
		"max_escalated_blocks":              p.MaxEscalatedBlocks,
		"report_refile_cooldown_blocks":     p.ReportRefileCooldownBlocks,
		"reporter_rate_limit_window_blocks": p.ReporterRateLimitWindowBlocks,
	}
	for name, v := range blocksFields {
		if v <= 0 {
			return ErrInvalidParams.Wrapf("%s must be > 0, got %d", name, v)
		}
	}

	// All *_bps in (0, 10000].
	bpsFields := map[string]uint32{
		"default_unilateral_slash_cap_bps": p.DefaultUnilateralSlashCapBps,
		"default_tier1_aggregate_cap_bps":  p.DefaultTier1AggregateCapBps,
	}
	for name, v := range bpsFields {
		if v == 0 || v > 10000 {
			return ErrInvalidParams.Wrapf("%s must be in (0, 10000], got %d", name, v)
		}
	}

	// default_tier1_aggregate_cap_bps >= default_unilateral_slash_cap_bps.
	if p.DefaultTier1AggregateCapBps < p.DefaultUnilateralSlashCapBps {
		return ErrInvalidParams.Wrapf("default_tier1_aggregate_cap_bps (%d) < default_unilateral_slash_cap_bps (%d)",
			p.DefaultTier1AggregateCapBps, p.DefaultUnilateralSlashCapBps)
	}

	// default_unbonding_period_blocks >= report_contest_window_blocks
	// (otherwise an operator could unbond before they could contest).
	if p.DefaultUnbondingPeriodBlocks < p.ReportContestWindowBlocks {
		return ErrInvalidParams.Wrapf("default_unbonding_period_blocks (%d) < report_contest_window_blocks (%d)",
			p.DefaultUnbondingPeriodBlocks, p.ReportContestWindowBlocks)
	}

	// report_refile_cooldown_blocks >= max_pending_blocks (§3.4.5 — the
	// refile-cooldown protection must outlive the dismissal window).
	if p.ReportRefileCooldownBlocks < p.MaxPendingBlocks {
		return ErrInvalidParams.Wrapf("report_refile_cooldown_blocks (%d) < max_pending_blocks (%d)",
			p.ReportRefileCooldownBlocks, p.MaxPendingBlocks)
	}

	// max_escalated_blocks > max_pending_blocks (juries take longer than
	// controllers).
	if p.MaxEscalatedBlocks <= p.MaxPendingBlocks {
		return ErrInvalidParams.Wrapf("max_escalated_blocks (%d) must be > max_pending_blocks (%d)",
			p.MaxEscalatedBlocks, p.MaxPendingBlocks)
	}

	// Size caps with hard upper bounds (proto / event sanity).
	if p.MaxMetadataBytes == 0 || p.MaxMetadataBytes > 65536 {
		return ErrInvalidParams.Wrapf("max_metadata_bytes must be in (0, 65536], got %d", p.MaxMetadataBytes)
	}
	if p.MaxReasonBytes == 0 || p.MaxReasonBytes > 4096 {
		return ErrInvalidParams.Wrapf("max_reason_bytes must be in (0, 4096], got %d", p.MaxReasonBytes)
	}

	// Report deposit must be a positive bond-denom amount. The denom
	// is stamped at the point of use by the keeper from x/identity.
	if p.ReportDepositAmount.IsNil() || !p.ReportDepositAmount.IsPositive() {
		return ErrInvalidParams.Wrap("report_deposit_amount must be > 0")
	}

	// Reporter rate-limit cap >= 1.
	if p.MaxReportsPerReporterPerOperatorPerWindow == 0 {
		return ErrInvalidParams.Wrap("max_reports_per_reporter_per_operator_per_window must be >= 1")
	}

	// Operator cap >= 1 (zero would block all registrations).
	if p.MaxActiveOperatorsPerAddress == 0 {
		return ErrInvalidParams.Wrap("max_active_operators_per_address must be >= 1")
	}

	// EndBlocker sweep limit >= 1 (zero would stall all sweeps forever).
	if p.EndblockerSweepLimit == 0 {
		return ErrInvalidParams.Wrap("endblocker_sweep_limit must be >= 1")
	}

	// Reputation accrual: a nil LegacyDec round-trips to non-equal
	// post-marshal (caught by tests) and is also operationally
	// meaningless. Allow zero (no accrual) but reject nil.
	if p.ReputationGrantPerBondBlock.IsNil() {
		return ErrInvalidParams.Wrap("reputation_grant_per_bond_block must be set (use 0 for no accrual)")
	}
	if p.ReputationGrantPerBondBlock.IsNegative() {
		return ErrInvalidParams.Wrap("reputation_grant_per_bond_block must be non-negative")
	}

	// Pagination: positive default, default ≤ max, max ≤ 10000.
	if p.DefaultPaginationLimit == 0 {
		return ErrInvalidParams.Wrap("default_pagination_limit must be > 0")
	}
	if p.MaxPaginationLimit < p.DefaultPaginationLimit {
		return ErrInvalidParams.Wrapf("max_pagination_limit (%d) < default_pagination_limit (%d)",
			p.MaxPaginationLimit, p.DefaultPaginationLimit)
	}
	if p.MaxPaginationLimit > 10000 {
		return ErrInvalidParams.Wrapf("max_pagination_limit must be <= 10000, got %d", p.MaxPaginationLimit)
	}

	// System-report rate-limit window must be positive when the cap is
	// nonzero (zero cap means "unlimited" and the window is irrelevant).
	if p.MaxSystemReportsPerCallerPerWindow > 0 && p.RateLimitWindowBlocks <= 0 {
		return ErrInvalidParams.Wrapf(
			"rate_limit_window_blocks must be > 0 when max_system_reports_per_caller_per_window (%d) is positive",
			p.MaxSystemReportsPerCallerPerWindow,
		)
	}

	return nil
}
