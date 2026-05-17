package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/service/types"
)

// InitGenesis loads the module state from a genesis snapshot.
// See x-service-spec.md §7 for the schema and validation rules.
//
// Order matters: params first (so any downstream validation has them),
// then service types (operators reference them), then operators (which
// hydrate the secondary indexes via PutOperator/ArchiveOperator), then
// reports + escrows + cases, then counters last.
//
// Note: the cross-record validations from §7 (orphan escrow refs, etc.)
// are enforced in types.GenesisState.Validate, not here — InitGenesis
// trusts a validated state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	if err := k.Params.Set(ctx, genState.Params); err != nil {
		return err
	}

	// Service type registry.
	for _, cfg := range genState.ServiceTypes {
		if err := k.ServiceTypes.Set(ctx, cfg.ServiceType, cfg); err != nil {
			return err
		}
	}

	// Live operators — PutOperator hydrates all secondary indexes
	// (OperatorsByController, OperatorsByServiceType, UnderfundedQueue).
	for _, op := range genState.Operators {
		if err := k.PutOperator(ctx, op); err != nil {
			return err
		}
	}

	// Archived operators — write directly to the archive store; no
	// secondary-index work needed since archived records aren't in the
	// live-operator indexes.
	for _, op := range genState.ArchivedOperators {
		addrBytes, err := k.addrBytes(op.Address)
		if err != nil {
			return err
		}
		if err := k.ArchivedOperators.Set(
			ctx,
			collections.Join3(addrBytes, op.ServiceType, op.RetiredAt),
			op,
		); err != nil {
			return err
		}
	}

	// Reports + ReportsByOperator + queue indexes.
	for _, r := range genState.Reports {
		if err := k.Reports.Set(ctx, r.ReportId, r); err != nil {
			return err
		}
		opBytes, err := k.addrBytes(r.OperatorAddress)
		if err != nil {
			return err
		}
		if err := k.ReportsByOperator.Set(ctx, collections.Join3(opBytes, r.ServiceType, r.ReportId)); err != nil {
			return err
		}
		switch r.Status {
		case types.ReportStatus_REPORT_STATUS_PENDING:
			if err := k.PendingReportsQueue.Set(ctx, collections.Join(r.FiledAt, r.ReportId)); err != nil {
				return err
			}
		case types.ReportStatus_REPORT_STATUS_ESCALATED:
			if r.EscalatedAt > 0 {
				if err := k.EscalatedReportsQueue.Set(ctx, collections.Join(r.EscalatedAt, r.ReportId)); err != nil {
					return err
				}
			}
		}
	}

	// Tier-1 escrow + per-operator + release-queue indexes.
	for _, e := range genState.Tier1Escrow {
		if err := k.Tier1Escrow.Set(ctx, e.EscrowId, e); err != nil {
			return err
		}
		opBytes, err := k.addrBytes(e.OperatorAddress)
		if err != nil {
			return err
		}
		if err := k.Tier1EscrowByOperator.Set(ctx, collections.Join3(opBytes, e.ServiceType, e.EscrowId)); err != nil {
			return err
		}
		if err := k.Tier1EscrowReleaseQueue.Set(ctx, collections.Join(e.ReleaseAt, e.EscrowId)); err != nil {
			return err
		}
	}

	// Controller-transfer cases + open-by-operator index.
	for _, c := range genState.ControllerTransferCases {
		if err := k.ControllerTransferCases.Set(ctx, c.JuryCaseId, c); err != nil {
			return err
		}
		opBytes, err := k.addrBytes(c.OperatorAddress)
		if err != nil {
			return err
		}
		if err := k.OpenControllerTransferByOperator.Set(
			ctx, collections.Join(opBytes, c.ServiceType), c.JuryCaseId,
		); err != nil {
			return err
		}
	}

	// Reporter rate-limit ring buffers.
	for _, rl := range genState.ReporterRateLimits {
		reporterBytes, err := k.addrBytes(rl.Reporter)
		if err != nil {
			return err
		}
		opBytes, err := k.addrBytes(rl.OperatorAddress)
		if err != nil {
			return err
		}
		if err := k.ReporterRateLimit.Set(
			ctx, collections.Join3(reporterBytes, opBytes, rl.ServiceType), rl,
		); err != nil {
			return err
		}
	}

	// Refile cooldowns.
	for _, rc := range genState.RefileCooldowns {
		controllerBytes, err := k.addrBytes(rc.Controller)
		if err != nil {
			return err
		}
		opBytes, err := k.addrBytes(rc.OperatorAddress)
		if err != nil {
			return err
		}
		if err := k.RefileCooldowns.Set(
			ctx, collections.Join4(controllerBytes, opBytes, rc.ServiceType, rc.DismissedAt), rc,
		); err != nil {
			return err
		}
	}

	// Tier1LastSlash.
	for _, ts := range genState.Tier1LastSlash {
		controllerBytes, err := k.addrBytes(ts.Controller)
		if err != nil {
			return err
		}
		opBytes, err := k.addrBytes(ts.OperatorAddress)
		if err != nil {
			return err
		}
		if err := k.Tier1LastSlash.Set(
			ctx, collections.Join3(controllerBytes, opBytes, ts.ServiceType), ts,
		); err != nil {
			return err
		}
	}

	// Counters last — Sequence.Set jumps the counter to the provided
	// value; subsequent .Next() returns counter+1. We want the next
	// generated id to equal the genesis counter, so set to counter-1
	// (saturating to 0).
	if genState.NextReportId > 0 {
		if err := k.NextReportID.Set(ctx, genState.NextReportId-1); err != nil {
			return err
		}
	}
	if genState.NextEscrowId > 0 {
		if err := k.NextEscrowID.Set(ctx, genState.NextEscrowId-1); err != nil {
			return err
		}
	}

	return nil
}

// ExportGenesis snapshots the module state into a GenesisState. The
// resulting snapshot, when fed back through InitGenesis, reproduces the
// same state (modulo lazy-update fields like LastBondBlockUpdateAt
// which are settled at next-event time).
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	var err error

	genesis := types.DefaultGenesis()
	genesis.Params, err = k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Service types.
	if genesis.ServiceTypes, err = collectMapValues(ctx, k.ServiceTypes); err != nil {
		return nil, err
	}

	// Live operators.
	if err := collectMapValuesInto(ctx, k.Operators, &genesis.Operators); err != nil {
		return nil, err
	}

	// Archived operators.
	if err := collectMapValuesInto(ctx, k.ArchivedOperators, &genesis.ArchivedOperators); err != nil {
		return nil, err
	}

	// Reports.
	if err := collectMapValuesInto(ctx, k.Reports, &genesis.Reports); err != nil {
		return nil, err
	}

	// Tier1 escrow.
	if err := collectMapValuesInto(ctx, k.Tier1Escrow, &genesis.Tier1Escrow); err != nil {
		return nil, err
	}

	// Controller-transfer cases.
	if err := collectMapValuesInto(ctx, k.ControllerTransferCases, &genesis.ControllerTransferCases); err != nil {
		return nil, err
	}

	// Reporter rate-limit ring buffers.
	if err := collectMapValuesInto(ctx, k.ReporterRateLimit, &genesis.ReporterRateLimits); err != nil {
		return nil, err
	}

	// Refile cooldowns.
	if err := collectMapValuesInto(ctx, k.RefileCooldowns, &genesis.RefileCooldowns); err != nil {
		return nil, err
	}

	// Tier1 last slash.
	if err := collectMapValuesInto(ctx, k.Tier1LastSlash, &genesis.Tier1LastSlash); err != nil {
		return nil, err
	}

	// Counters — Peek returns the most-recently-issued id; the next
	// generated id will be one higher, so the genesis "next" field
	// should be peek+1.
	nextReport, err := k.NextReportID.Peek(ctx)
	if err == nil {
		genesis.NextReportId = nextReport + 1
	}
	nextEscrow, err := k.NextEscrowID.Peek(ctx)
	if err == nil {
		genesis.NextEscrowId = nextEscrow + 1
	}

	return genesis, nil
}

// collectMapValues walks a collections.Map and returns all values in
// store order. Generic helper for ExportGenesis.
func collectMapValues[K, V any](ctx context.Context, m collections.Map[K, V]) ([]V, error) {
	iter, err := m.Iterate(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	var out []V
	for ; iter.Valid(); iter.Next() {
		v, err := iter.Value()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// collectMapValuesInto is the variant that appends into an existing
// slice pointer — convenient when assigning back into a struct field
// without an extra type-parameter inference dance at the call site.
func collectMapValuesInto[K, V any](ctx context.Context, m collections.Map[K, V], dst *[]V) error {
	out, err := collectMapValues(ctx, m)
	if err != nil {
		return err
	}
	*dst = out
	return nil
}
