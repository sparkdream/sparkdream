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

	// BondDenom is the canonical SPARK micro-denom. x/service is SPARK-only
	// (see x-service-spec.md §1 SPARK-bonded principle).
	BondDenom = "uspark"
)

// Service-operator "bonded" tag — written/deducted by x/service to grant
// or deduct operator reputation (§6.6). The tag is registered in x/rep's
// reserved-tag genesis so operators don't pay the creation fee.
const ReputationTagServiceOperator = "service-operator"

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
