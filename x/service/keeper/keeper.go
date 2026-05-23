package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"

	"sparkdream/x/service/types"
)

// Keeper holds all state for x/service. See x-service-spec.md §4.1 for the
// full store layout and §3.6 for the eager-vs-EndBlocker transition split.
type Keeper struct {
	storeService corestore.KVStoreService
	cdc          codec.Codec
	addressCodec address.Codec

	// authority is the address allowed to call MsgUpdateParams /
	// MsgUpdateServiceTypeConfig — set to x/gov module account at wiring
	// time. See §5.3.
	authority []byte

	Schema collections.Schema
	Params collections.Item[types.Params]

	// bankKeeper is wired at construction time (no cycle risk) and is
	// safe to keep as a value field.
	bankKeeper types.BankKeeper

	// authKeeper provides GetModuleAddress used by OpenSystemReport's
	// forward-derive caller authorization (Phase 0 of the federation→
	// service migration). Wired at construction time, no cycle risk.
	authKeeper types.AuthKeeper

	// External-module dependencies wired AFTER NewAppModule via
	// SetCrossModuleKeepers / SetHooks. They live behind a shared
	// pointer so post-construction wiring is visible to every copy of
	// the Keeper (notably the AppModule's snapshot copy used by the
	// msg-server — see docs/development-conventions.md "AppModule Value-Copy Bug").
	//
	// Handlers that need a cross-module keeper must nil-check via the
	// accessor helpers below; nil means standalone mode (early
	// development / unit tests).
	late *lateKeepers

	// ----- Primary stores (§4.1) -----

	// Operators: live records keyed by (address_bytes, service_type).
	// Holds only non-terminal records (ACTIVE | UNDERFUNDED | UNBONDING).
	Operators collections.Map[collections.Pair[[]byte, string], types.Operator]

	// ArchivedOperators: terminal records (SLASHED + RETIRED) keyed by
	// (address_bytes, service_type, retired_at). The triple-key shape
	// allows multiple terminal records over time on the same
	// (address, service_type) pair.
	ArchivedOperators collections.Map[collections.Triple[[]byte, string, int64], types.Operator]

	// ServiceTypes: governance-managed allowlist.
	ServiceTypes collections.Map[string, types.ServiceTypeConfig]

	// Reports: keyed by report_id (auto-incrementing).
	Reports collections.Map[uint64, types.Report]

	// Tier1Escrow: in-flight tier-1 slashes keyed by escrow_id.
	Tier1Escrow collections.Map[uint64, types.Tier1EscrowEntry]

	// ControllerTransferCases: open controller-transfer cases keyed by
	// jury_case_id (§5.4).
	ControllerTransferCases collections.Map[uint64, types.ControllerTransferCase]

	// RefileCooldowns: per-(controller, op, service_type, dismissed_at).
	// Lazy expiry — consulted at MsgReportOperator time (§3.4.5).
	RefileCooldowns collections.Map[collections.Quad[[]byte, []byte, string, int64], types.RefileCooldown]

	// ReporterRateLimit: sliding-window ring buffer keyed by
	// (reporter, op, service_type) (§3.4.6).
	ReporterRateLimit collections.Map[collections.Triple[[]byte, []byte, string], types.ReporterRateLimit]

	// Tier1LastSlash: per-(controller, op, service_type), the height of
	// the last tier-1 slash; consulted for cooldown enforcement
	// (§3.4.4). Pruned at operator archival.
	Tier1LastSlash collections.Map[collections.Triple[[]byte, []byte, string], types.Tier1LastSlash]

	// ----- Secondary indexes (§4.1) -----

	// OperatorsByController: (controller, op_address, service_type).
	OperatorsByController collections.KeySet[collections.Triple[[]byte, []byte, string]]

	// OperatorsByServiceType: (service_type, op_address).
	OperatorsByServiceType collections.KeySet[collections.Pair[string, []byte]]

	// UnderfundedQueue: (underfunded_since, op_address, service_type).
	// EndBlocker sweep iterates in (height) order (§3.6 queue 1).
	UnderfundedQueue collections.KeySet[collections.Triple[int64, []byte, string]]

	// ReportsByOperator: (op_address, service_type, report_id).
	ReportsByOperator collections.KeySet[collections.Triple[[]byte, string, uint64]]

	// PendingReportsQueue: (filed_at, report_id) — EndBlocker queue 2.
	PendingReportsQueue collections.KeySet[collections.Pair[int64, uint64]]

	// EscalatedReportsQueue: (escalated_at, report_id) — EndBlocker queue 3.
	EscalatedReportsQueue collections.KeySet[collections.Pair[int64, uint64]]

	// OpenControllerTransferByOperator: (op_address, service_type) →
	// jury_case_id. Enforces the "one open case at a time" rule (§5.4).
	OpenControllerTransferByOperator collections.Map[collections.Pair[[]byte, string], uint64]

	// Tier1EscrowByOperator: (op_address, service_type, escrow_id).
	// Used by MsgClaimUnbondedBond and contest paths.
	Tier1EscrowByOperator collections.KeySet[collections.Triple[[]byte, string, uint64]]

	// Tier1EscrowReleaseQueue: (release_at, escrow_id) — EndBlocker
	// queue 4.
	Tier1EscrowReleaseQueue collections.KeySet[collections.Pair[int64, uint64]]

	// SystemReportDedup: (caller_module, dedupe_key) → report_id. Enables
	// idempotent OpenSystemReport calls; a re-call with the same key
	// resolves to the existing report instead of opening a new one.
	// Pruned when the existing report transitions to a terminal state
	// (auto-dismiss / resolved / timeout) and the TTL elapses; kept
	// minimal because dedupe is meaningful only within the report's
	// lifetime window.
	SystemReportDedup collections.Map[collections.Pair[string, []byte], uint64]

	// SystemReportRateLimit: sliding-window ring buffer keyed by
	// caller_module. Tracks recent OpenSystemReport call heights so the
	// keeper can enforce max_system_reports_per_caller_per_window.
	SystemReportRateLimit collections.Map[string, types.SystemReportRateLimit]

	// ----- Counters -----

	NextReportID collections.Sequence
	NextEscrowID collections.Sequence
}

// lateKeepers holds cross-module dependencies that are wired into the
// service Keeper AFTER depinject has run (and therefore AFTER the
// AppModule has snapshotted its keeper). All Keeper copies share the
// same *lateKeepers, so SetCrossModuleKeepers / SetHooks is observed
// uniformly.
type lateKeepers struct {
	commonsKeeper      types.CommonsKeeper
	repKeeper          types.RepKeeper
	distributionKeeper types.DistributionKeeper
	identityKeeper     types.IdentityKeeper
	hooks              types.ServiceHooks
}

// NewKeeper constructs the service Keeper. Cross-module keepers
// (commons, rep, distribution) are wired in app.go after construction
// via SetCrossModuleKeepers to avoid depinject cycles.
//
// authKeeper is taken at construction time so OpenSystemReport's
// forward-derive caller authorization can resolve allowlisted module
// names to their canonical module-account addresses. authKeeper may be
// nil in early-development standalone configurations; OpenSystemReport
// will reject all calls if so.
func NewKeeper(
	storeService corestore.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,

	bankKeeper types.BankKeeper,
	authKeeper types.AuthKeeper,
) Keeper {
	if _, err := addressCodec.BytesToString(authority); err != nil {
		panic(fmt.Sprintf("invalid authority address %s: %s", authority, err))
	}

	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		storeService: storeService,
		cdc:          cdc,
		addressCodec: addressCodec,
		authority:    authority,

		bankKeeper: bankKeeper,
		authKeeper: authKeeper,
		late:       &lateKeepers{},

		Params: collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),

		// Primary stores.
		Operators: collections.NewMap(
			sb,
			types.OperatorsKey,
			"operators",
			collections.PairKeyCodec(collections.BytesKey, collections.StringKey),
			codec.CollValue[types.Operator](cdc),
		),
		ArchivedOperators: collections.NewMap(
			sb,
			types.ArchivedOperatorsKey,
			"archived_operators",
			collections.TripleKeyCodec(collections.BytesKey, collections.StringKey, collections.Int64Key),
			codec.CollValue[types.Operator](cdc),
		),
		ServiceTypes: collections.NewMap(
			sb,
			types.ServiceTypesKey,
			"service_types",
			collections.StringKey,
			codec.CollValue[types.ServiceTypeConfig](cdc),
		),
		Reports: collections.NewMap(
			sb,
			types.ReportsKey,
			"reports",
			collections.Uint64Key,
			codec.CollValue[types.Report](cdc),
		),
		Tier1Escrow: collections.NewMap(
			sb,
			types.Tier1EscrowKey,
			"tier1_escrow",
			collections.Uint64Key,
			codec.CollValue[types.Tier1EscrowEntry](cdc),
		),
		ControllerTransferCases: collections.NewMap(
			sb,
			types.ControllerTransferCasesKey,
			"controller_transfer_cases",
			collections.Uint64Key,
			codec.CollValue[types.ControllerTransferCase](cdc),
		),
		RefileCooldowns: collections.NewMap(
			sb,
			types.RefileCooldownsKey,
			"refile_cooldowns",
			collections.QuadKeyCodec(collections.BytesKey, collections.BytesKey, collections.StringKey, collections.Int64Key),
			codec.CollValue[types.RefileCooldown](cdc),
		),
		ReporterRateLimit: collections.NewMap(
			sb,
			types.ReporterRateLimitKey,
			"reporter_rate_limit",
			collections.TripleKeyCodec(collections.BytesKey, collections.BytesKey, collections.StringKey),
			codec.CollValue[types.ReporterRateLimit](cdc),
		),
		Tier1LastSlash: collections.NewMap(
			sb,
			types.Tier1LastSlashKey,
			"tier1_last_slash",
			collections.TripleKeyCodec(collections.BytesKey, collections.BytesKey, collections.StringKey),
			codec.CollValue[types.Tier1LastSlash](cdc),
		),

		// Secondary indexes.
		OperatorsByController: collections.NewKeySet(
			sb,
			types.OperatorsByControllerKey,
			"operators_by_controller",
			collections.TripleKeyCodec(collections.BytesKey, collections.BytesKey, collections.StringKey),
		),
		OperatorsByServiceType: collections.NewKeySet(
			sb,
			types.OperatorsByServiceTypeKey,
			"operators_by_service_type",
			collections.PairKeyCodec(collections.StringKey, collections.BytesKey),
		),
		UnderfundedQueue: collections.NewKeySet(
			sb,
			types.UnderfundedQueueKey,
			"underfunded_queue",
			collections.TripleKeyCodec(collections.Int64Key, collections.BytesKey, collections.StringKey),
		),
		ReportsByOperator: collections.NewKeySet(
			sb,
			types.ReportsByOperatorKey,
			"reports_by_operator",
			collections.TripleKeyCodec(collections.BytesKey, collections.StringKey, collections.Uint64Key),
		),
		PendingReportsQueue: collections.NewKeySet(
			sb,
			types.PendingReportsQueueKey,
			"pending_reports_queue",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key),
		),
		EscalatedReportsQueue: collections.NewKeySet(
			sb,
			types.EscalatedReportsQueueKey,
			"escalated_reports_queue",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key),
		),
		OpenControllerTransferByOperator: collections.NewMap(
			sb,
			types.OpenControllerTransferByOperatorKey,
			"open_controller_transfer_by_operator",
			collections.PairKeyCodec(collections.BytesKey, collections.StringKey),
			collections.Uint64Value,
		),
		Tier1EscrowByOperator: collections.NewKeySet(
			sb,
			types.Tier1EscrowByOperatorKey,
			"tier1_escrow_by_operator",
			collections.TripleKeyCodec(collections.BytesKey, collections.StringKey, collections.Uint64Key),
		),
		Tier1EscrowReleaseQueue: collections.NewKeySet(
			sb,
			types.Tier1EscrowReleaseQueueKey,
			"tier1_escrow_release_queue",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key),
		),
		SystemReportDedup: collections.NewMap(
			sb,
			types.SystemReportDedupKey,
			"system_report_dedup",
			collections.PairKeyCodec(collections.StringKey, collections.BytesKey),
			collections.Uint64Value,
		),
		SystemReportRateLimit: collections.NewMap(
			sb,
			types.SystemReportRateLimitKey,
			"system_report_rate_limit",
			collections.StringKey,
			codec.CollValue[types.SystemReportRateLimit](cdc),
		),

		// Counters.
		NextReportID: collections.NewSequence(sb, types.NextReportIDKey, "next_report_id"),
		NextEscrowID: collections.NewSequence(sb, types.NextEscrowIDKey, "next_escrow_id"),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

// GetAuthority returns the module's authority (x/gov module account).
func (k Keeper) GetAuthority() []byte {
	return k.authority
}

// SetCrossModuleKeepers wires cross-module dependencies after construction.
// Called from app.go after all module keepers are built. Avoids depinject
// cycles where x/service ↔ x/commons / x/rep / x/distribution have
// to-be-determined wiring order. Handlers that need a cross-module keeper
// must nil-check via the accessors below; nil means the module is
// operating in standalone mode (early development / tests).
func (k Keeper) SetCrossModuleKeepers(
	commonsKeeper types.CommonsKeeper,
	repKeeper types.RepKeeper,
	distributionKeeper types.DistributionKeeper,
) {
	k.late.commonsKeeper = commonsKeeper
	k.late.repKeeper = repKeeper
	k.late.distributionKeeper = distributionKeeper
}

// SetHooks registers the service hooks implementation. Called once from
// app.go after the consuming keepers (notably x/commons for
// AfterOperatorDissolved) are constructed. See §6.1.
func (k Keeper) SetHooks(hooks types.ServiceHooks) Keeper {
	if k.late.hooks != nil {
		panic("cannot set service hooks twice")
	}
	k.late.hooks = hooks
	return k
}

// SetIdentityKeeper wires the x/identity keeper post-depinject so the
// keeper can resolve the chain's bond denom at runtime.
func (k Keeper) SetIdentityKeeper(ik types.IdentityKeeper) {
	k.late.identityKeeper = ik
}

// BondDenom returns the chain's bond denom from the wired identity
// keeper. Panics if identity is not wired — every call site needs a real
// denom and silently returning a hardcoded literal would re-introduce
// the legacy mixed-state bug.
func (k Keeper) BondDenom(ctx context.Context) string {
	if k.late == nil || k.late.identityKeeper == nil {
		panic("service keeper: identityKeeper not wired (call SetIdentityKeeper after depinject)")
	}
	return k.late.identityKeeper.BondDenom(ctx)
}

// commonsKeeper returns the wired CommonsKeeper, or nil if running in
// standalone mode.
func (k Keeper) commonsKeeper() types.CommonsKeeper {
	if k.late == nil {
		return nil
	}
	return k.late.commonsKeeper
}

// repKeeper returns the wired RepKeeper, or nil.
func (k Keeper) repKeeper() types.RepKeeper {
	if k.late == nil {
		return nil
	}
	return k.late.repKeeper
}

// distributionKeeper returns the wired DistributionKeeper, or nil.
func (k Keeper) distributionKeeper() types.DistributionKeeper {
	if k.late == nil {
		return nil
	}
	return k.late.distributionKeeper
}

// hooks returns the wired ServiceHooks, or nil.
func (k Keeper) hooks() types.ServiceHooks {
	if k.late == nil {
		return nil
	}
	return k.late.hooks
}
