package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

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
		if err := k.ensureUnderfundedDue(ctx, op, addrBytes); err != nil {
			return err
		}
	} else {
		// Best-effort removal — Remove on a non-existent key is a no-op
		// in collections.
		if err := k.UnderfundedQueue.Remove(ctx, queueKey); err != nil {
			return err
		}
		k.clearUnderfundedDue(ctx, addrBytes, op.ServiceType)
	}

	return nil
}

// ensureUnderfundedDue files an UNDERFUNDED operator under its grace
// deadline (underfunded_since + the service type's grace) unless it is
// already filed. The sweep re-checks the grace with the current params when
// the deadline comes, so a stale deadline only ever delays the forced
// unbond (a longer grace re-files it; a shorter one applies from the old
// deadline).
func (k Keeper) ensureUnderfundedDue(ctx context.Context, op types.Operator, addrBytes []byte) error {
	revKey := collections.Join(addrBytes, op.ServiceType)
	if has, err := k.UnderfundedDueAt.Has(ctx, revKey); err != nil || has {
		return err
	}
	deadline := op.UnderfundedSince
	if params, err := k.Params.Get(ctx); err == nil {
		if cfg, cErr := k.resolveServiceTypeConfig(ctx, op.ServiceType); cErr == nil {
			deadline = op.UnderfundedSince + k.effectiveUnderfundedGraceBlocks(cfg, params)
		}
	}
	return k.setUnderfundedDue(ctx, addrBytes, op.ServiceType, deadline)
}

// setUnderfundedDue (re)files the operator under deadline.
func (k Keeper) setUnderfundedDue(ctx context.Context, addrBytes []byte, serviceType string, deadline int64) error {
	k.clearUnderfundedDue(ctx, addrBytes, serviceType)
	if err := k.UnderfundedDue.Set(ctx, collections.Join3(deadline, addrBytes, serviceType)); err != nil {
		return err
	}
	return k.UnderfundedDueAt.Set(ctx, collections.Join(addrBytes, serviceType), deadline)
}

// clearUnderfundedDue drops the operator's due entry, if any.
func (k Keeper) clearUnderfundedDue(ctx context.Context, addrBytes []byte, serviceType string) {
	revKey := collections.Join(addrBytes, serviceType)
	if deadline, err := k.UnderfundedDueAt.Get(ctx, revKey); err == nil {
		if err := k.UnderfundedDue.Remove(ctx, collections.Join3(deadline, addrBytes, serviceType)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("clearUnderfundedDue: UnderfundedDue.Remove failed", "err", err)
		}
		if err := k.UnderfundedDueAt.Remove(ctx, revKey); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("clearUnderfundedDue: UnderfundedDueAt.Remove failed", "err", err)
		}
	}
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

	if err := k.OperatorsByController.Remove(ctx, collections.Join3(ctlBytes, addrBytes, op.ServiceType)); err != nil {
		return err
	}
	if err := k.OperatorsByServiceType.Remove(ctx, collections.Join(op.ServiceType, addrBytes)); err != nil {
		return err
	}
	if op.UnderfundedSince > 0 {
		if err := k.UnderfundedQueue.Remove(ctx, collections.Join3(op.UnderfundedSince, addrBytes, op.ServiceType)); err != nil {
			return err
		}
	}
	k.clearUnderfundedDue(ctx, addrBytes, op.ServiceType)
	return nil
}

// ArchiveOperator moves a live record to the archive store and prunes
// per-operator slash history. Used for both terminal transitions
// (SLASHED via slash → 0, or jury dissolve; RETIRED via successful
// claim).
//
// The caller MUST set op.Status, op.RetiredAt, and op.BondAmount (zero
// for archive — see §3.1 archived-records-always-have-bond-zero rule)
// before invoking this function.
func (k Keeper) ArchiveOperator(ctx context.Context, op types.Operator) error {
	if op.RetiredAt == 0 {
		return errors.New("ArchiveOperator: retired_at must be set")
	}
	if !op.BondAmount.IsZero() {
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
	k.clearUnderfundedDue(ctx, opAddr, serviceType)
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
// any mutation to op.BondAmount or op.Status (see §6.6).
//
// Callers MUST persist the operator after settling (this function
// mutates the passed Operator in place; it does NOT call PutOperator).
func (k Keeper) settleBondBlocks(op *types.Operator, currentHeight int64) {
	if op.Status == types.OperatorStatus_OPERATOR_STATUS_ACTIVE && op.LastBondBlockUpdateAt > 0 {
		elapsed := currentHeight - op.LastBondBlockUpdateAt
		if elapsed > 0 && !op.BondAmount.IsNil() && !op.BondAmount.IsZero() {
			delta := op.BondAmount.Mul(sdkmath.NewInt(elapsed))
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

// pendingWithdraw returns the operator's queued partial withdrawal,
// treating a nil Int (pre-field records, hand-written genesis) as zero.
func pendingWithdraw(op types.Operator) sdkmath.Int {
	if op.PendingWithdrawAmount.IsNil() {
		return sdkmath.ZeroInt()
	}
	return op.PendingWithdrawAmount
}

// clearPendingWithdrawal drops any queued partial withdrawal (§3.5a). Called
// on every exit from ACTIVE into UNBONDING or a terminal status, where the
// exit path returns or confiscates the whole bond instead.
func clearPendingWithdrawal(op *types.Operator) {
	op.PendingWithdrawAmount = sdkmath.ZeroInt()
	op.WithdrawCompleteAt = 0
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

// setControllerTransferCase stores an open controller-transfer case and its
// opened_at index entry.
func (k Keeper) setControllerTransferCase(ctx context.Context, c types.ControllerTransferCase) error {
	if err := k.ControllerTransferCases.Set(ctx, c.JuryCaseId, c); err != nil {
		return err
	}
	return k.ControllerTransferCasesByOpenedAt.Set(ctx, collections.Join(c.OpenedAt, c.JuryCaseId))
}

// removeControllerTransferCase deletes a case and its opened_at index entry.
func (k Keeper) removeControllerTransferCase(ctx context.Context, c types.ControllerTransferCase) error {
	if err := k.ControllerTransferCases.Remove(ctx, c.JuryCaseId); err != nil {
		return err
	}
	return k.ControllerTransferCasesByOpenedAt.Remove(ctx, collections.Join(c.OpenedAt, c.JuryCaseId))
}

// markUnderfundedIfBelowMin moves an ACTIVE operator whose bond is below its
// service type's current min_bond to UNDERFUNDED, starting the grace period
// at height. It changes op in memory only and reports whether it did; the
// caller persists op with PutOperator (which files the grace deadline),
// then emits the event and fires AfterOperatorUnderfunded.
//
// Governance can raise min_bond at any time, and the config update does not
// walk the registry (spec §3.6: the transition is lazy, on the next message
// touching the operator). Accrual must already be settled to height.
func markUnderfundedIfBelowMin(op *types.Operator, cfg types.ServiceTypeConfig, height int64) bool {
	if op.Status != types.OperatorStatus_OPERATOR_STATUS_ACTIVE || !op.BondAmount.LT(cfg.MinBondAmount) {
		return false
	}
	op.Status = types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED
	op.UnderfundedSince = height
	return true
}

// reconcileMinBond applies markUnderfundedIfBelowMin to a stored operator and
// persists the transition, with its event and hook. Operator-touching message
// handlers call it before acting, so a raised min_bond takes effect on the
// operator's next interaction. Missing operators and configs are left to the
// caller's own checks.
func (k Keeper) reconcileMinBond(ctx context.Context, opBytes []byte, serviceType string) error {
	op, exists := k.GetOperator(ctx, opBytes, serviceType)
	if !exists || op.Status != types.OperatorStatus_OPERATOR_STATUS_ACTIVE {
		return nil
	}
	cfg, err := k.resolveServiceTypeConfig(ctx, serviceType)
	if err != nil {
		return nil
	}
	if !op.BondAmount.LT(cfg.MinBondAmount) {
		return nil
	}
	height := sdk.UnwrapSDKContext(ctx).BlockHeight()
	k.settleBondBlocks(&op, height)
	markUnderfundedIfBelowMin(&op, cfg, height)
	if err := k.PutOperator(ctx, op); err != nil {
		return err
	}
	k.emitUnderfunded(ctx, op, cfg, height)
	if k.hooks() != nil {
		k.hooks().AfterOperatorUnderfunded(ctx, sdk.AccAddress(opBytes), serviceType)
	}
	return nil
}

// emitUnderfunded emits operator_underfunded for an operator that just
// entered UNDERFUNDED at height.
func (k Keeper) emitUnderfunded(ctx context.Context, op types.Operator, cfg types.ServiceTypeConfig, height int64) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return
	}
	bondDenom := k.BondDenom(ctx)
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(types.NewOperatorUnderfundedEvent(
		op.Address, op.ServiceType, sdk.NewCoin(bondDenom, op.BondAmount), sdk.NewCoin(bondDenom, cfg.MinBondAmount),
		height+k.effectiveUnderfundedGraceBlocks(cfg, params),
	))
}
