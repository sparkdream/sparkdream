package types

import "cosmossdk.io/collections"

const (
	// ModuleName defines the module name.
	ModuleName = "service"

	// StoreKey defines the primary module store key.
	StoreKey = ModuleName

	// GovModuleName duplicates the gov module's name to avoid a hard
	// dependency on x/gov. Sync with cosmos-sdk if ever renamed.
	// https://github.com/cosmos/cosmos-sdk/blob/v0.52.0-beta.2/x/gov/types/keys.go#L9
	GovModuleName = "gov"

	// RepModuleName is the x/rep module's account name. Sync with
	// x/rep/types/keys.go.
	RepModuleName = "rep"

	// FederationModuleName is the x/federation module's account name.
	// Used by the OpenSystemReport caller allowlist (Phase 0). Sync
	// with x/federation/types/keys.go.
	FederationModuleName = "federation"
)

// Service-operator "bonded" tag — written/deducted by x/service to grant
// or deduct operator reputation (§6.6). The tag is seeded in x/rep's
// default genesis as a reserved tag (reptypes.ReservedTagServiceOperator)
// so no member can create or squat it; keep the two constants in sync.
const ReputationTagServiceOperator = "service-operator"

// JurySlashReputationMultiplier scales the §6.6 reputation deduction for a
// tier-2 (jury) slash relative to a tier-1 slash of the same size. A jury
// verdict is an adjudicated finding, not a controller's unilateral call, so
// it carries a heavier mark on the operator's record. Compiled rather than a
// param: it is a policy constant, not an operating knob. The deduction is
// still capped at the reputation the record has accrued.
const JurySlashReputationMultiplier = 2

// ParamsKey is the prefix used by the Params collections.Item.
var ParamsKey = collections.NewPrefix("p_service")

// ---------------------------------------------------------------------------
// Primary stores (see x-service-spec.md §4.1).
// ---------------------------------------------------------------------------

var (
	// OperatorsKey: live operator records keyed by (address_bytes, service_type).
	OperatorsKey = collections.NewPrefix("operators/value/")

	// ArchivedOperatorsKey: SLASHED + RETIRED records keyed by
	// (address_bytes, service_type, retired_at) to allow multiple terminal
	// records over time.
	ArchivedOperatorsKey = collections.NewPrefix("archived_operators/value/")

	// ServiceTypesKey: governance-managed allowlist keyed by service_type.
	ServiceTypesKey = collections.NewPrefix("service_types/value/")

	// ReportsKey: reports keyed by report_id (auto-increment).
	ReportsKey = collections.NewPrefix("reports/value/")

	// Tier1EscrowKey: in-flight tier-1 slashes keyed by escrow_id
	// (auto-increment).
	Tier1EscrowKey = collections.NewPrefix("tier1_escrow/value/")

	// ControllerTransferCasesKey: open controller-transfer cases keyed by
	// jury_case_id.
	ControllerTransferCasesKey = collections.NewPrefix("controller_transfer_cases/value/")

	// RefileCooldownsKey: keyed by (controller, operator, service_type,
	// dismissed_at). Lazy expiry.
	RefileCooldownsKey = collections.NewPrefix("refile_cooldowns/value/")

	// ReporterRateLimitKey: sliding-window ring buffer keyed by
	// (reporter, operator, service_type).
	ReporterRateLimitKey = collections.NewPrefix("reporter_rate_limit/value/")

	// Tier1LastSlashKey: last tier-1 slash height keyed by
	// (controller, operator, service_type). Pruned at archive.
	Tier1LastSlashKey = collections.NewPrefix("tier1_last_slash/value/")

	// SystemReportDedupKey: (caller_module, dedupe_key) → report_id.
	// Powers idempotent OpenSystemReport (Phase 0 of the federation→
	// service migration).
	SystemReportDedupKey = collections.NewPrefix("system_report_dedup/value/")

	// SystemReportRateLimitKey: sliding-window ring buffer keyed by
	// caller_module. Powers the per-caller cap on OpenSystemReport
	// filings.
	SystemReportRateLimitKey = collections.NewPrefix("system_report_rate_limit/value/")

	// SystemReportDedupExpiryKey: (eligible_at_height, caller_module,
	// dedupe_key). The EndBlocker drops a dedup entry once its report is
	// no longer PENDING/ESCALATED and the retention has elapsed.
	SystemReportDedupExpiryKey = collections.NewPrefix("idx/system_report_dedup_expiry/")
)

// ---------------------------------------------------------------------------
// Secondary indexes (KeySet/Map; see §4.1).
// ---------------------------------------------------------------------------

var (
	// OperatorsByControllerKey: (controller, address, service_type).
	OperatorsByControllerKey = collections.NewPrefix("idx/operators_controller/")

	// OperatorsByServiceTypeKey: (service_type, address).
	OperatorsByServiceTypeKey = collections.NewPrefix("idx/operators_service_type/")

	// UnderfundedQueueKey: (underfunded_since, address, service_type) —
	// EndBlocker sweep ordering (§3.6 queue 1).
	UnderfundedQueueKey = collections.NewPrefix("idx/underfunded_queue/")

	// UnderfundedDueKey: (grace_deadline, address, service_type) — the
	// forced-unbond sweep walks it up to the current height, so it visits
	// only operators whose grace has run out (SVC-07).
	// UnderfundedDueAtKey: (address, service_type) -> grace_deadline, the
	// reverse entry that lets a status change drop the due key.
	// Derived state: PutOperator maintains both, InitGenesis rebuilds them.
	UnderfundedDueKey   = collections.NewPrefix("idx/underfunded_due/")
	UnderfundedDueAtKey = collections.NewPrefix("idx/underfunded_due_at/")

	// ReportsByOperatorKey: (operator, service_type, report_id).
	ReportsByOperatorKey = collections.NewPrefix("idx/reports_operator/")

	// PendingReportsQueueKey: (filed_at, report_id) — EndBlocker
	// auto-dismiss sweep (§3.6 queue 2).
	PendingReportsQueueKey = collections.NewPrefix("idx/pending_reports/")

	// EscalatedReportsQueueKey: (escalated_at, report_id) — EndBlocker
	// auto-timeout sweep (§3.6 queue 3).
	EscalatedReportsQueueKey = collections.NewPrefix("idx/escalated_reports/")

	// OpenControllerTransferByOperatorKey: (operator, service_type) →
	// jury_case_id. Enforces "one open case at a time" (§5.4).
	OpenControllerTransferByOperatorKey = collections.NewPrefix("idx/open_controller_transfer/")

	// Tier1EscrowByOperatorKey: (operator, service_type, escrow_id).
	Tier1EscrowByOperatorKey = collections.NewPrefix("idx/tier1_escrow_operator/")

	// Tier1EscrowReleaseQueueKey: (release_at, escrow_id) — EndBlocker
	// release sweep (§3.6 queue 4).
	Tier1EscrowReleaseQueueKey = collections.NewPrefix("idx/tier1_escrow_release/")

	// CheckpointsKey: (service_type, op_address) → Checkpoint.
	CheckpointsKey = collections.NewPrefix("checkpoints/value/")

	// CheckpointDeadlinesKey: (deadline_height, service_type, op_address)
	// — EndBlocker liveness queue for service types with
	// checkpoint_max_lag_blocks > 0.
	CheckpointDeadlinesKey = collections.NewPrefix("idx/checkpoint_deadlines/")

	// CheckpointDeadlineByOperatorKey: (service_type, op_address) →
	// deadline_height. Reverse of CheckpointDeadlines so rescheduling can
	// drop the old queue entry without a scan.
	CheckpointDeadlineByOperatorKey = collections.NewPrefix("idx/checkpoint_deadline_operator/")

	// ControllerTransferCasesByOpenedAtKey: (opened_at, jury_case_id).
	// The timeout sweep reads it in opened_at order and stops at the first
	// case not yet due, so it never walks undue cases.
	ControllerTransferCasesByOpenedAtKey = collections.NewPrefix("idx/controller_transfer_opened_at/")

	// SweepParkedKey: (retry_at, sweep, item) — EndBlocker items whose last
	// attempt failed, parked until retry_at so they cannot hold the head of
	// their queue. The prune pass walks it up to the current
	// height.
	SweepParkedKey = collections.NewPrefix("idx/sweep_parked/")

	// SweepParkedAtKey: (sweep, item) → retry_at; reverse of SweepParked,
	// read by the sweeps to skip a parked item without a scan.
	SweepParkedAtKey = collections.NewPrefix("idx/sweep_parked_at/")
)

// ---------------------------------------------------------------------------
// Auto-incrementing counters.
// ---------------------------------------------------------------------------

var (
	// NextReportIDKey: collections.Sequence for Reports primary key.
	NextReportIDKey = collections.NewPrefix("next_report_id/")

	// NextEscrowIDKey: collections.Sequence for Tier1Escrow primary key.
	NextEscrowIDKey = collections.NewPrefix("next_escrow_id/")
)

// MaxSlashBps is 100% of a bond in basis points: the ceiling on any
// proposed or applied slash_bps.
const MaxSlashBps = 10000
