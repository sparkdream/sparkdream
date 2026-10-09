package types

import (
	"fmt"

	"cosmossdk.io/math"
)

// DefaultGenesis returns the default genesis state.
//
// Seeds the content-scanner service type (docs/content-scanning.md §8) and
// four ServiceTypeConfig entries for the federation→service migration
// (Phase 8 of the migration plan):
//   - federation-bridge-activitypub
//   - federation-bridge-atproto
//   - federation-bridge-nostr
//   - federation-bridge-lens
//
// Both use the same default knobs initially (matching x/service module
// params); per Decision 1 of the migration plan they may diverge via
// gov MsgUpdateServiceTypeConfig if the risk model dictates. Both opt
// into report_timeout_action=ESCALATE so a silent controller can't
// park a slash forever (Decision 3).
func DefaultGenesis() *GenesisState {
	params := DefaultParams()
	return &GenesisState{
		Params:       params,
		ServiceTypes: append(defaultFederationBridgeServiceTypes(params), defaultContentScannerServiceType(params)),
	}
}

// ContentScannerServiceType is the service type of content-scanner workers
// (docs/content-scanning.md §8).
const ContentScannerServiceType = "content-scanner"

// defaultContentScannerServiceType seeds the content-scanner service type
// in its rollout-phase-2 shape: attestation_quorum = 1, because the first
// worker is one the council controls directly; gov raises it once several
// independent workers run (phase 3). Liveness reports fire when a worker's
// checkpoint trails the chain by about a day. Lapses are routine,
// council-judged matters, so stalled reports DISMISS rather than escalate.
func defaultContentScannerServiceType(params Params) ServiceTypeConfig {
	// 100 SPARK: scanners are hired by a council and paid by recurring
	// spend; the bond backs liveness and accuracy, not custody of funds.
	const minBondUspark = int64(100_000_000)
	// Roughly one day: the default unbonding period is ~14 days.
	lag := params.DefaultUnbondingPeriodBlocks / 14
	return ServiceTypeConfig{
		ServiceType:              ContentScannerServiceType,
		Description:              "Off-chain content scanner publishing signed media verdicts (docs/content-scanning.md)",
		MinBondAmount:            math.NewInt(minBondUspark),
		UnbondingPeriodBlocks:    params.DefaultUnbondingPeriodBlocks,
		UnilateralSlashCapBps:    params.DefaultUnilateralSlashCapBps,
		Tier1WindowBlocks:        params.DefaultTier1WindowBlocks,
		Tier1AggregateCapBps:     params.DefaultTier1AggregateCapBps,
		Tier1CooldownBlocks:      params.DefaultTier1CooldownBlocks,
		UnderfundedGraceBlocks:   params.DefaultUnderfundedGraceBlocks,
		Enabled:                  true,
		ReportTimeoutAction:      ReportTimeoutAction_REPORT_TIMEOUT_ACTION_DISMISS,
		ChallengeDefaultSlashBps: 100,
		AttestationQuorum:        1,
		CheckpointMaxLagBlocks:   lag,
	}
}

// defaultFederationBridgeServiceTypes returns the genesis seed for the
// two federation-bridge service_types. Public so app/genesis_*.go can
// override values per build tag (mainnet/testnet/devnet/testparams)
// without having to re-derive the structure.
func defaultFederationBridgeServiceTypes(params Params) []ServiceTypeConfig {
	// 1000 SPARK default (mainnet target). Build-tagged genesis_vals
	// can override per environment if/when needed.
	const defaultMinBondUspark = int64(1_000_000_000)
	// 1% — conservative starting slash. Controllers may adjust upward
	// up to unilateral_slash_cap_bps at resolve time.
	const defaultChallengeDefaultSlashBps uint32 = 100

	mkCfg := func(serviceType, description string) ServiceTypeConfig {
		return ServiceTypeConfig{
			ServiceType:              serviceType,
			Description:              description,
			MinBondAmount:            math.NewInt(defaultMinBondUspark),
			UnbondingPeriodBlocks:    params.DefaultUnbondingPeriodBlocks,
			UnilateralSlashCapBps:    params.DefaultUnilateralSlashCapBps,
			Tier1WindowBlocks:        params.DefaultTier1WindowBlocks,
			Tier1AggregateCapBps:     params.DefaultTier1AggregateCapBps,
			Tier1CooldownBlocks:      params.DefaultTier1CooldownBlocks,
			UnderfundedGraceBlocks:   params.DefaultUnderfundedGraceBlocks,
			Enabled:                  true,
			ReportTimeoutAction:      ReportTimeoutAction_REPORT_TIMEOUT_ACTION_ESCALATE,
			ChallengeDefaultSlashBps: defaultChallengeDefaultSlashBps,
		}
	}
	return []ServiceTypeConfig{
		mkCfg("federation-bridge-activitypub", "Off-chain bridge operator for ActivityPub federation"),
		mkCfg("federation-bridge-atproto", "Off-chain bridge operator for AT Protocol federation"),
		mkCfg("federation-bridge-nostr", "Off-chain bridge operator for NOSTR federation"),
		mkCfg("federation-bridge-lens", "Off-chain bridge operator for Lens Protocol federation"),
	}
}

// Validate performs genesis state validation per x-service-spec.md §7.
// Returns nil iff all cross-record invariants hold; otherwise wraps the
// first failing rule.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}

	// service_type registry uniqueness + presence build-set.
	serviceTypes := make(map[string]struct{}, len(gs.ServiceTypes))
	for _, cfg := range gs.ServiceTypes {
		if cfg.ServiceType == "" {
			return fmt.Errorf("genesis: service_types entry has empty service_type")
		}
		if _, dup := serviceTypes[cfg.ServiceType]; dup {
			return fmt.Errorf("genesis: duplicate service_type %q", cfg.ServiceType)
		}
		serviceTypes[cfg.ServiceType] = struct{}{}
	}

	// Live operators: bond > 0, valid status, no self-controller,
	// (address, service_type) unique.
	liveOps := make(map[string]struct{}, len(gs.Operators))
	for _, op := range gs.Operators {
		key := op.Address + "/" + op.ServiceType
		if _, dup := liveOps[key]; dup {
			return fmt.Errorf("genesis: duplicate live operator %s", key)
		}
		liveOps[key] = struct{}{}

		if _, exists := serviceTypes[op.ServiceType]; !exists {
			return fmt.Errorf("genesis: operator %s references unknown service_type %q",
				op.Address, op.ServiceType)
		}
		if op.Controller == op.Address {
			return fmt.Errorf("genesis: operator %s has self-controller", op.Address)
		}
		switch op.Status {
		case OperatorStatus_OPERATOR_STATUS_ACTIVE,
			OperatorStatus_OPERATOR_STATUS_UNDERFUNDED,
			OperatorStatus_OPERATOR_STATUS_UNBONDING:
			// OK.
		default:
			return fmt.Errorf("genesis: live operator %s has invalid status %s",
				op.Address, op.Status)
		}
		if op.BondAmount.IsNil() || !op.BondAmount.IsPositive() {
			return fmt.Errorf("genesis: live operator %s bond must be positive", op.Address)
		}
	}

	// Archived operators: status must be terminal, retired_at > 0, bond
	// must be zero, NO collision with live operators on
	// (address, service_type).
	for _, op := range gs.ArchivedOperators {
		key := op.Address + "/" + op.ServiceType
		if _, collision := liveOps[key]; collision {
			return fmt.Errorf("genesis: archived operator %s collides with live record", key)
		}
		if _, exists := serviceTypes[op.ServiceType]; !exists {
			return fmt.Errorf("genesis: archived operator %s references unknown service_type %q",
				op.Address, op.ServiceType)
		}
		switch op.Status {
		case OperatorStatus_OPERATOR_STATUS_SLASHED,
			OperatorStatus_OPERATOR_STATUS_RETIRED:
			// OK.
		default:
			return fmt.Errorf("genesis: archived operator %s has non-terminal status %s",
				op.Address, op.Status)
		}
		if op.RetiredAt <= 0 {
			return fmt.Errorf("genesis: archived operator %s retired_at must be > 0", op.Address)
		}
		if !op.BondAmount.IsNil() && !op.BondAmount.IsZero() {
			return fmt.Errorf("genesis: archived operator %s bond must be zero", op.Address)
		}
	}

	// Reports: status validity, ESCALATED implies (escalated_at>0,
	// jury_case_id!=0), cross-ref to service_types.
	reportIDs := make(map[uint64]struct{}, len(gs.Reports))
	maxReportID := uint64(0)
	for _, r := range gs.Reports {
		if _, dup := reportIDs[r.ReportId]; dup {
			return fmt.Errorf("genesis: duplicate report_id %d", r.ReportId)
		}
		reportIDs[r.ReportId] = struct{}{}
		if r.ReportId > maxReportID {
			maxReportID = r.ReportId
		}

		if _, exists := serviceTypes[r.ServiceType]; !exists {
			return fmt.Errorf("genesis: report %d references unknown service_type %q",
				r.ReportId, r.ServiceType)
		}
		if r.Status == ReportStatus_REPORT_STATUS_ESCALATED {
			if r.EscalatedAt <= 0 {
				return fmt.Errorf("genesis: ESCALATED report %d has zero escalated_at", r.ReportId)
			}
			// jury_case_id may be zero in standalone dev mode; soft check.
		}
	}
	if gs.NextReportId > 0 && gs.NextReportId <= maxReportID {
		return fmt.Errorf("genesis: next_report_id %d must be > max report_id %d", gs.NextReportId, maxReportID)
	}

	// Tier1Escrow: every entry must reference a known report on a known
	// (operator, service_type). The operator must exist in either the
	// live or archived set (a slash escrow without any operator record
	// is state corruption).
	allOps := make(map[string]struct{}, len(gs.Operators)+len(gs.ArchivedOperators))
	for _, op := range gs.Operators {
		allOps[op.Address+"/"+op.ServiceType] = struct{}{}
	}
	for _, op := range gs.ArchivedOperators {
		allOps[op.Address+"/"+op.ServiceType] = struct{}{}
	}
	maxEscrowID := uint64(0)
	for _, e := range gs.Tier1Escrow {
		if _, ok := reportIDs[e.ReportId]; !ok {
			return fmt.Errorf("genesis: tier1_escrow %d references unknown report_id %d",
				e.EscrowId, e.ReportId)
		}
		if _, ok := allOps[e.OperatorAddress+"/"+e.ServiceType]; !ok {
			return fmt.Errorf("genesis: tier1_escrow %d references unknown operator %s/%s",
				e.EscrowId, e.OperatorAddress, e.ServiceType)
		}
		if e.EscrowId > maxEscrowID {
			maxEscrowID = e.EscrowId
		}
	}
	if gs.NextEscrowId > 0 && gs.NextEscrowId <= maxEscrowID {
		return fmt.Errorf("genesis: next_escrow_id %d must be > max escrow_id %d", gs.NextEscrowId, maxEscrowID)
	}

	// Controller-transfer cases: at most one open case per
	// (operator, service_type).
	openCases := make(map[string]struct{}, len(gs.ControllerTransferCases))
	for _, c := range gs.ControllerTransferCases {
		key := c.OperatorAddress + "/" + c.ServiceType
		if _, dup := openCases[key]; dup {
			return fmt.Errorf("genesis: multiple open controller-transfer cases for %s", key)
		}
		openCases[key] = struct{}{}
		if _, ok := allOps[key]; !ok {
			return fmt.Errorf("genesis: controller-transfer case %d references unknown operator %s",
				c.JuryCaseId, key)
		}
	}

	// Checkpoints: one per (service_type, operator), for a known service
	// type, with a positive height and a 32-byte root. The operator may be
	// archived (checkpoints outlive their operator as an audit trail).
	seenCheckpoints := make(map[string]struct{}, len(gs.Checkpoints))
	for _, cp := range gs.Checkpoints {
		key := cp.ServiceType + "/" + cp.Operator
		if _, dup := seenCheckpoints[key]; dup {
			return fmt.Errorf("genesis: duplicate checkpoint for %s", key)
		}
		seenCheckpoints[key] = struct{}{}
		if _, ok := serviceTypes[cp.ServiceType]; !ok {
			return fmt.Errorf("genesis: checkpoint for %s references unknown service_type", key)
		}
		if cp.Height <= 0 || len(cp.Root) != 32 {
			return fmt.Errorf("genesis: checkpoint for %s needs height > 0 and a 32-byte root", key)
		}
	}

	return nil
}
