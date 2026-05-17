package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"

	"sparkdream/x/service/types"
)

// state.go holds the cross-cutting state-machine primitives: bond-block
// settlement (§6.6 lazy O(1) accrual), live↔archive transitions, and the
// secondary-index maintenance that every write path MUST go through.
//
// IMPORTANT: never write to k.Operators / k.ArchivedOperators directly —
// always go through PutOperator / ArchiveOperator so secondary indexes
// stay consistent.

// addrBytes converts a bech32 string to its raw bytes for use as a
// store key. Returns an error if the address is malformed; callers
// should treat any error as ErrInvalidSigner-level (the caller should
// have validated before reaching state).
func (k Keeper) addrBytes(addr string) ([]byte, error) {
	return k.addressCodec.StringToBytes(addr)
}

// operatorKey returns the primary-store key for an operator.
func (k Keeper) operatorKey(addrBytes []byte, serviceType string) collections.Pair[[]byte, string] {
	return collections.Join(addrBytes, serviceType)
}

// GetOperator returns the live operator record for (addr, serviceType)
// or false if missing. Live-store only — terminal records live in
// ArchivedOperators (see GetArchivedOperators).
func (k Keeper) GetOperator(ctx context.Context, addrBytes []byte, serviceType string) (types.Operator, bool) {
	op, err := k.Operators.Get(ctx, k.operatorKey(addrBytes, serviceType))
	if err != nil {
		return types.Operator{}, false
	}
	return op, true
}

// HasSlashedRecord reports whether (addr, serviceType) has any SLASHED
// record in ArchivedOperators — used by registration to block re-entry
// on a previously-slashed pair (§3.1). RETIRED records do NOT block
// re-registration.
//
// Implementation: iterate the K1=addrBytes prefix on ArchivedOperators
// and filter by K2=serviceType and status. We cannot constrain on K1+K2
// at the range level (collections.NewPrefixedTripleRange only prefixes
// on K1), so we filter in-loop. The K1 prefix bounds the scan to one
// address's archive (typically <10 rows).
func (k Keeper) HasSlashedRecord(ctx context.Context, addrBytes []byte, serviceType string) (bool, error) {
	rng := collections.NewPrefixedTripleRange[[]byte, string, int64](addrBytes)
	iter, err := k.ArchivedOperators.Iterate(ctx, rng)
	if err != nil {
		return false, err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return false, err
		}
		if key.K2() != serviceType {
			continue
		}
		op, err := k.ArchivedOperators.Get(ctx, key)
		if err != nil {
			return false, err
		}
		if op.Status == types.OperatorStatus_OPERATOR_STATUS_SLASHED {
			return true, nil
		}
	}
	return false, nil
}

// CountLiveOperatorsForAddress returns how many live records exist for
// the given address across all service types. Used by registration to
// enforce max_active_operators_per_address (§5.1).
func (k Keeper) CountLiveOperatorsForAddress(ctx context.Context, addrBytes []byte) (uint32, error) {
	// Iterate the Operators primary store with a prefix on the address
	// component of the (addr, service_type) composite key.
	rng := collections.NewPrefixedPairRange[[]byte, string](addrBytes)
	iter, err := k.Operators.Iterate(ctx, rng)
	if err != nil {
		return 0, err
	}
	defer iter.Close()

	var n uint32
	for ; iter.Valid(); iter.Next() {
		n++
	}
	return n, nil
}

// PutOperator writes a live operator record and maintains all secondary
// indexes. Idempotent — safe to call on update as long as the operator's
// `controller` and `service_type` fields haven't changed (they're keyed
// in the indexes).
func (k Keeper) PutOperator(ctx context.Context, op types.Operator) error {
	addrBytes, err := k.addrBytes(op.Address)
	if err != nil {
		return err
	}
	ctlBytes, err := k.addrBytes(op.Controller)
	if err != nil {
		return err
	}

	if err := k.Operators.Set(ctx, k.operatorKey(addrBytes, op.ServiceType), op); err != nil {
		return err
	}
	if err := k.OperatorsByController.Set(ctx, collections.Join3(ctlBytes, addrBytes, op.ServiceType)); err != nil {
		return err
	}
	if err := k.OperatorsByServiceType.Set(ctx, collections.Join(op.ServiceType, addrBytes)); err != nil {
		return err
	}

	// UnderfundedQueue membership is tied to status — add/remove as
	// status changes. PutOperator handles both directions.
	queueKey := collections.Join3(op.UnderfundedSince, addrBytes, op.ServiceType)
	if op.Status == types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED && op.UnderfundedSince > 0 {
		if err := k.UnderfundedQueue.Set(ctx, queueKey); err != nil {
			return err
		}
	} else {
		// Best-effort removal — Remove on a non-existent key is a no-op
		// in collections.
		_ = k.UnderfundedQueue.Remove(ctx, queueKey)
	}

	return nil
}

// removeFromLiveIndexes deletes (address, service_type)-related entries
// from secondary indexes. Used when transitioning to a terminal status
// (ArchiveOperator) before deleting the primary row.
func (k Keeper) removeFromLiveIndexes(ctx context.Context, op types.Operator) error {
	addrBytes, err := k.addrBytes(op.Address)
	if err != nil {
		return err
	}
	ctlBytes, err := k.addrBytes(op.Controller)
	if err != nil {
		return err
	}

	_ = k.OperatorsByController.Remove(ctx, collections.Join3(ctlBytes, addrBytes, op.ServiceType))
	_ = k.OperatorsByServiceType.Remove(ctx, collections.Join(op.ServiceType, addrBytes))
	if op.UnderfundedSince > 0 {
		_ = k.UnderfundedQueue.Remove(ctx, collections.Join3(op.UnderfundedSince, addrBytes, op.ServiceType))
	}
	return nil
}

// ArchiveOperator moves a live record to the archive store and prunes
// per-operator slash history. Used for both terminal transitions
// (SLASHED via slash → 0, or jury dissolve; RETIRED via successful
// claim).
//
// The caller MUST set op.Status, op.RetiredAt, and op.Bond (zero for
// archive — see §3.1 archived-records-always-have-bond-zero rule)
// before invoking this function.
func (k Keeper) ArchiveOperator(ctx context.Context, op types.Operator) error {
	if op.RetiredAt == 0 {
		return errors.New("ArchiveOperator: retired_at must be set")
	}
	if !op.Bond.Amount.IsZero() {
		return errors.New("ArchiveOperator: bond must be zero on archive")
	}
	if op.Status != types.OperatorStatus_OPERATOR_STATUS_SLASHED && op.Status != types.OperatorStatus_OPERATOR_STATUS_RETIRED {
		return errors.New("ArchiveOperator: status must be SLASHED or RETIRED")
	}

	addrBytes, err := k.addrBytes(op.Address)
	if err != nil {
		return err
	}

	// Drop secondary indexes for the live record first.
	if err := k.removeFromLiveIndexes(ctx, op); err != nil {
		return err
	}

	// Delete from live store, write to archive.
	if err := k.Operators.Remove(ctx, k.operatorKey(addrBytes, op.ServiceType)); err != nil {
		return err
	}
	if err := k.ArchivedOperators.Set(ctx, collections.Join3(addrBytes, op.ServiceType, op.RetiredAt), op); err != nil {
		return err
	}

	// Prune per-operator tier-1 cooldown history (§4.1) — pruned on
	// BOTH SLASHED and RETIRED so a re-registered operator never
	// inherits cooldown state from a prior incarnation.
	if err := k.pruneTier1LastSlashForOperator(ctx, addrBytes, op.ServiceType); err != nil {
		return err
	}

	return nil
}

// removeUnderfundedQueueEntry drops the UnderfundedQueue entry for
// the (height, op_addr, service_type) tuple. No-op if the key is
// missing. Used at status transitions out of UNDERFUNDED.
func (k Keeper) removeUnderfundedQueueEntry(ctx context.Context, underfundedSince int64, opAddr []byte, serviceType string) error {
	return k.UnderfundedQueue.Remove(ctx, collections.Join3(underfundedSince, opAddr, serviceType))
}

// pruneTier1LastSlashForOperator removes all Tier1LastSlash entries
// for the given (operator, service_type) across all controllers.
func (k Keeper) pruneTier1LastSlashForOperator(ctx context.Context, opAddr []byte, serviceType string) error {
	// Tier1LastSlash is keyed by (controller, operator, service_type).
	// To prune by (operator, service_type), iterate the whole map and
	// match — small per-archive cost, no secondary index needed.
	iter, err := k.Tier1LastSlash.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	defer iter.Close()

	var toDelete []collections.Triple[[]byte, []byte, string]
	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return err
		}
		if string(key.K2()) == string(opAddr) && key.K3() == serviceType {
			toDelete = append(toDelete, key)
		}
	}
	for _, key := range toDelete {
		if err := k.Tier1LastSlash.Remove(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// settleBondBlocks updates the operator's total_lifetime_bond_blocks
// from (current_bond * elapsed_active_blocks). MUST be called before
// any mutation to op.Bond or op.Status (see §6.6).
//
// Callers MUST persist the operator after settling (this function
// mutates the passed Operator in place; it does NOT call PutOperator).
func (k Keeper) settleBondBlocks(op *types.Operator, currentHeight int64) {
	if op.Status == types.OperatorStatus_OPERATOR_STATUS_ACTIVE && op.LastBondBlockUpdateAt > 0 {
		elapsed := currentHeight - op.LastBondBlockUpdateAt
		if elapsed > 0 && !op.Bond.Amount.IsNil() && !op.Bond.Amount.IsZero() {
			delta := op.Bond.Amount.Mul(sdkmath.NewInt(elapsed))
			if op.TotalLifetimeBondBlocks.IsNil() {
				op.TotalLifetimeBondBlocks = sdkmath.ZeroInt()
			}
			op.TotalLifetimeBondBlocks = op.TotalLifetimeBondBlocks.Add(delta)
		}
	}
	// Status entry/exit to ACTIVE — UNDERFUNDED, UNBONDING, terminal:
	// no accrual but timestamp still updates so the next ACTIVE period
	// (if any) doesn't double-count.
	op.LastBondBlockUpdateAt = currentHeight
}

// resolveServiceTypeConfig fetches the per-type config or returns
// ErrServiceTypeNotFound.
func (k Keeper) resolveServiceTypeConfig(ctx context.Context, serviceType string) (types.ServiceTypeConfig, error) {
	cfg, err := k.ServiceTypes.Get(ctx, serviceType)
	if err != nil {
		return types.ServiceTypeConfig{}, types.ErrServiceTypeNotFound.Wrap(serviceType)
	}
	return cfg, nil
}

// effectiveUnbondingPeriodBlocks returns the per-type override or the
// global default.
func (k Keeper) effectiveUnbondingPeriodBlocks(cfg types.ServiceTypeConfig, params types.Params) int64 {
	if cfg.UnbondingPeriodBlocks > 0 {
		return cfg.UnbondingPeriodBlocks
	}
	return params.DefaultUnbondingPeriodBlocks
}

// effectiveUnderfundedGraceBlocks returns the per-type override or the
// global default.
func (k Keeper) effectiveUnderfundedGraceBlocks(cfg types.ServiceTypeConfig, params types.Params) int64 {
	if cfg.UnderfundedGraceBlocks > 0 {
		return cfg.UnderfundedGraceBlocks
	}
	return params.DefaultUnderfundedGraceBlocks
}
