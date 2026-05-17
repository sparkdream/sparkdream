package keeper

import (
	"context"
	"time"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/service/types"
)

// EndBlocker runs the four sweeps described in x-service-spec.md §3.6.
// Each sweep is gas-bounded (endblocker_sweep_limit per category per
// block) and iterates in deterministic key order; if more records are
// eligible than the limit allows, the remainder is processed next
// block.
//
// Sweep order matters:
//  1. Underfunded → forced-unbond (operators that missed the grace).
//  2. Pending report auto-dismiss (controller stalls).
//  3. Escalated report auto-timeout (jury inaction).
//  4. Tier-1 escrow release (post-contest-window funds → community pool).
//
// Sweep 5 (unbond completion) is NOT performed — it's lazy via
// MsgClaimUnbondedBond (§3.6).
//
// The function returns the first error encountered. Errors in a sweep
// abort the rest of that sweep's batch but other sweeps still run, so
// one failing record doesn't stall the whole block.
func (k Keeper) EndBlocker(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	limit := int(params.EndblockerSweepLimit)
	if limit <= 0 {
		// Defensive; Params.Validate rejects zero, but defaults to 100
		// if somehow not set.
		limit = 100
	}

	var firstErr error

	runSweep := func(name string, fn func() (int, error)) {
		start := time.Now()
		n, err := fn()
		emitSweepDuration(name, start)
		if n > 0 {
			emitSweptRecords(name, float32(n))
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	runSweep("underfunded", func() (int, error) {
		return k.sweepUnderfunded(ctx, params, currentHeight, limit)
	})
	runSweep("pending_reports", func() (int, error) {
		return k.sweepPendingReports(ctx, params, currentHeight, limit)
	})
	runSweep("escalated_reports", func() (int, error) {
		return k.sweepEscalatedReports(ctx, params, currentHeight, limit)
	})
	runSweep("tier1_escrow_release", func() (int, error) {
		return k.sweepTier1EscrowRelease(ctx, currentHeight, limit)
	})

	return firstErr
}

// ---------------------------------------------------------------------------
// Sweep 1: Underfunded → forced UNBONDING (§3.6 queue 1)
// ---------------------------------------------------------------------------

// sweepUnderfunded force-unbonds UNDERFUNDED operators whose grace
// period has elapsed. Iterates UnderfundedQueue in (underfunded_since)
// order so the oldest victims are processed first. Returns the number
// of records actually transitioned for telemetry.
func (k Keeper) sweepUnderfunded(ctx context.Context, params types.Params, currentHeight int64, limit int) (int, error) {
	processed := 0
	iter, err := k.UnderfundedQueue.Iterate(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer iter.Close()

	type pending struct {
		queueKey collections.Triple[int64, []byte, string]
	}
	var batch []pending
	for ; iter.Valid() && len(batch) < limit; iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return processed, err
		}
		batch = append(batch, pending{queueKey: key})
	}

	for _, p := range batch {
		underfundedSince := p.queueKey.K1()
		opAddr := p.queueKey.K2()
		serviceType := p.queueKey.K3()

		// Re-fetch the operator and verify it's still UNDERFUNDED — a
		// concurrent message may have transitioned it (e.g. top-up).
		op, exists := k.GetOperator(ctx, opAddr, serviceType)
		if !exists {
			// Stale queue entry. Remove.
			_ = k.UnderfundedQueue.Remove(ctx, p.queueKey)
			continue
		}
		if op.Status != types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED {
			_ = k.UnderfundedQueue.Remove(ctx, p.queueKey)
			continue
		}
		cfg, err := k.resolveServiceTypeConfig(ctx, op.ServiceType)
		if err != nil {
			// Service type yanked from registry mid-flight; skip.
			continue
		}
		grace := k.effectiveUnderfundedGraceBlocks(cfg, params)
		if currentHeight < underfundedSince+grace {
			// Not yet expired.
			continue
		}

		// Force-unbond. Settle, flip status, set clock, drop the
		// UnderfundedQueue entry.
		k.settleBondBlocks(&op, currentHeight)
		op.Status = types.OperatorStatus_OPERATOR_STATUS_UNBONDING
		op.UnbondCompleteAt = currentHeight + k.effectiveUnbondingPeriodBlocks(cfg, params)
		op.UnderfundedSince = 0

		_ = k.UnderfundedQueue.Remove(ctx, p.queueKey)
		if err := k.PutOperator(ctx, op); err != nil {
			return processed, err
		}

		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(types.NewOperatorUnbondStartedEvent(
			op.Address, op.ServiceType, op.UnbondCompleteAt, types.UnbondSourceForcedUnderfunded,
		))
		emitOperatorUnbondStarted(op.ServiceType, types.UnbondSourceForcedUnderfunded)
		processed++
	}

	return processed, nil
}

// ---------------------------------------------------------------------------
// Sweep 2: PENDING report auto-dismiss (§3.6 queue 2)
// ---------------------------------------------------------------------------

// sweepPendingReports auto-dismisses PENDING reports older than
// max_pending_blocks. Deposit refunded to reporter; the controller is
// barred from re-filing the same allegation against this operator for
// report_refile_cooldown_blocks.
func (k Keeper) sweepPendingReports(ctx context.Context, params types.Params, currentHeight int64, limit int) (int, error) {
	processed := 0
	iter, err := k.PendingReportsQueue.Iterate(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer iter.Close()

	type pending struct {
		queueKey collections.Pair[int64, uint64]
		reportID uint64
		filedAt  int64
	}
	var batch []pending
	for ; iter.Valid() && len(batch) < limit; iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return processed, err
		}
		filedAt := key.K1()
		if currentHeight < filedAt+params.MaxPendingBlocks {
			// Sorted by filed_at — no further items will be eligible.
			break
		}
		batch = append(batch, pending{queueKey: key, reportID: key.K2(), filedAt: filedAt})
	}

	for _, p := range batch {
		report, err := k.Reports.Get(ctx, p.reportID)
		if err != nil {
			_ = k.PendingReportsQueue.Remove(ctx, p.queueKey)
			continue
		}
		if report.Status != types.ReportStatus_REPORT_STATUS_PENDING {
			_ = k.PendingReportsQueue.Remove(ctx, p.queueKey)
			continue
		}

		// Refund deposit to reporter.
		reporterBytes, err := k.addrBytes(report.Reporter)
		if err == nil && !report.Deposit.Amount.IsNil() && !report.Deposit.Amount.IsZero() {
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(
				ctx, types.ModuleName, sdk.AccAddress(reporterBytes), sdk.NewCoins(report.Deposit),
			); err != nil {
				return processed, err
			}
		}

		// Record refile cooldown against the operator's current controller.
		op, exists := k.GetOperator(ctx, mustAddrBytesK(k, report.OperatorAddress), report.ServiceType)
		if exists {
			if err := k.recordRefileCooldown(ctx, op.Controller, report.OperatorAddress, report.ServiceType, currentHeight); err != nil {
				return processed, err
			}
		}

		// Update report status, drop queue entry.
		report.Status = types.ReportStatus_REPORT_STATUS_AUTO_DISMISSED
		if err := k.Reports.Set(ctx, report.ReportId, report); err != nil {
			return processed, err
		}
		_ = k.PendingReportsQueue.Remove(ctx, p.queueKey)

		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
			types.NewReportAutoDismissedEvent(report.ReportId, report.Deposit),
		)
		processed++
	}

	return processed, nil
}

// ---------------------------------------------------------------------------
// Sweep 3: ESCALATED report auto-timeout (§3.6 queue 3)
// ---------------------------------------------------------------------------

// sweepEscalatedReports auto-resolves ESCALATED reports older than
// max_escalated_blocks as AUTO_TIMEOUT — treated as REJECT-equivalent
// (no slash; contested-T1 escrow returns to bond; reporter/opener
// deposit refunded; jury case canceled). See §3.6 + §3.4.5.
func (k Keeper) sweepEscalatedReports(ctx context.Context, params types.Params, currentHeight int64, limit int) (int, error) {
	processed := 0
	iter, err := k.EscalatedReportsQueue.Iterate(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer iter.Close()

	type pending struct {
		queueKey    collections.Pair[int64, uint64]
		reportID    uint64
		escalatedAt int64
	}
	var batch []pending
	for ; iter.Valid() && len(batch) < limit; iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return processed, err
		}
		escalatedAt := key.K1()
		if currentHeight < escalatedAt+params.MaxEscalatedBlocks {
			break
		}
		batch = append(batch, pending{queueKey: key, reportID: key.K2(), escalatedAt: escalatedAt})
	}

	for _, p := range batch {
		report, err := k.Reports.Get(ctx, p.reportID)
		if err != nil {
			_ = k.EscalatedReportsQueue.Remove(ctx, p.queueKey)
			continue
		}
		if report.Status != types.ReportStatus_REPORT_STATUS_ESCALATED {
			_ = k.EscalatedReportsQueue.Remove(ctx, p.queueKey)
			continue
		}

		opBytes, err := k.addrBytes(report.OperatorAddress)
		if err != nil {
			continue
		}

		// Release any contested-T1 escrow back to the operator's bond.
		op, exists := k.GetOperator(ctx, opBytes, report.ServiceType)
		if exists {
			escrow, escrowID, hasEscrow, err := k.findTier1EscrowForReportK(ctx, opBytes, report.ServiceType, report.ReportId)
			if err == nil && hasEscrow {
				k.settleBondBlocks(&op, currentHeight)
				op.Bond = op.Bond.Add(escrow.Amount)
				if !op.Tier1SlashedInWindow.IsNil() && !escrow.Amount.Amount.IsZero() {
					op.Tier1SlashedInWindow = op.Tier1SlashedInWindow.Sub(escrow.Amount.Amount)
					if op.Tier1SlashedInWindow.IsNegative() {
						op.Tier1SlashedInWindow = sdkmath.ZeroInt()
					}
				}
				// Status may revert UNDERFUNDED → ACTIVE if bond clears
				// min_bond again.
				if op.Status == types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED {
					if cfg, err := k.resolveServiceTypeConfig(ctx, op.ServiceType); err == nil {
						if op.Bond.Amount.GTE(cfg.MinBond.Amount) {
							oldUF := op.UnderfundedSince
							op.Status = types.OperatorStatus_OPERATOR_STATUS_ACTIVE
							op.UnderfundedSince = 0
							if oldUF > 0 {
								_ = k.removeUnderfundedQueueEntry(ctx, oldUF, opBytes, op.ServiceType)
							}
						}
					}
				}
				if err := k.PutOperator(ctx, op); err != nil {
					return processed, err
				}
				_ = k.Tier1Escrow.Remove(ctx, escrowID)
				_ = k.Tier1EscrowByOperator.Remove(ctx, collections.Join3(opBytes, op.ServiceType, escrowID))
				_ = k.Tier1EscrowReleaseQueue.Remove(ctx, collections.Join(escrow.ReleaseAt, escrowID))
			}
		}

		// Refund reporter deposit (AUTO_TIMEOUT is treated as REJECT-
		// equivalent for the reporter — they're not at fault for the
		// jury not deciding).
		if reporterBytes, err := k.addrBytes(report.Reporter); err == nil && !report.Deposit.Amount.IsZero() {
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(
				ctx, types.ModuleName, sdk.AccAddress(reporterBytes), sdk.NewCoins(report.Deposit),
			); err != nil {
				return processed, err
			}
		}

		// The parallel x/rep JuryReview is left in place — x/rep does
		// not expose a cancel API, and a still-PENDING JuryReview on
		// an AUTO_TIMEOUT'd report is harmless (the resolver-side
		// handlers check x/service's report status before applying any
		// verdict, so a late-arriving jury verdict against an already-
		// timed-out report would fail with ErrInvalidVerdict).

		// Update report status, drop queue entry.
		report.Status = types.ReportStatus_REPORT_STATUS_AUTO_TIMEOUT
		if err := k.Reports.Set(ctx, report.ReportId, report); err != nil {
			return processed, err
		}
		_ = k.EscalatedReportsQueue.Remove(ctx, p.queueKey)

		// AUTO_TIMEOUT is treated as REJECT-equivalent. Any contested-T1
		// escrow was already returned to bond by the inner block above;
		// it's removed from state too. We just need to surface the
		// returned-to-bond amount in the event for downstream auditing.
		// For now report zero since the actual contested amount was
		// already added to op.Bond inline; future versions could pass
		// the captured escrow value here.
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
			types.NewReportAutoTimeoutEvent(report.ReportId, report.Deposit, sdk.NewCoin(types.BondDenom, sdkmath.ZeroInt())),
		)
		processed++
	}

	return processed, nil
}

// ---------------------------------------------------------------------------
// Sweep 4: Tier-1 escrow release (§3.6 queue 4)
// ---------------------------------------------------------------------------

// sweepTier1EscrowRelease transfers escrowed SPARK to the community
// pool for any Tier1Escrow entries whose release_at has passed AND
// whose parent report is in a terminal state (RESOLVED_T1 uncontested,
// RESOLVED_T2, or AUTO_TIMEOUT — but AUTO_TIMEOUT escrows return to
// bond instead of community pool, handled by sweep 3 above).
func (k Keeper) sweepTier1EscrowRelease(ctx context.Context, currentHeight int64, limit int) (int, error) {
	processed := 0
	iter, err := k.Tier1EscrowReleaseQueue.Iterate(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer iter.Close()

	type pending struct {
		queueKey collections.Pair[int64, uint64]
		escrowID uint64
	}
	var batch []pending
	for ; iter.Valid() && len(batch) < limit; iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return processed, err
		}
		releaseAt := key.K1()
		if currentHeight < releaseAt {
			break
		}
		batch = append(batch, pending{queueKey: key, escrowID: key.K2()})
	}

	for _, p := range batch {
		escrow, err := k.Tier1Escrow.Get(ctx, p.escrowID)
		if err != nil {
			_ = k.Tier1EscrowReleaseQueue.Remove(ctx, p.queueKey)
			continue
		}

		// Parent report must be in a terminal state. If still
		// PENDING/ESCALATED/RESOLVED_T1 within window, skip — that
		// shouldn't happen given release_at, but defensive.
		report, err := k.Reports.Get(ctx, escrow.ReportId)
		if err != nil {
			_ = k.Tier1EscrowReleaseQueue.Remove(ctx, p.queueKey)
			continue
		}
		// AUTO_TIMEOUT already returned funds to bond via sweep 3 (above)
		// — defensive guard.
		if report.Status == types.ReportStatus_REPORT_STATUS_PENDING ||
			report.Status == types.ReportStatus_REPORT_STATUS_ESCALATED {
			continue
		}

		// Pay out to community pool.
		if k.distributionKeeper() != nil && !escrow.Amount.Amount.IsZero() {
			if err := k.distributionKeeper().FundCommunityPool(
				ctx, sdk.NewCoins(escrow.Amount), k.bankModuleAddress(),
			); err != nil {
				return processed, err
			}
		}

		opBytes, err := k.addrBytes(escrow.OperatorAddress)
		if err == nil {
			_ = k.Tier1EscrowByOperator.Remove(ctx, collections.Join3(opBytes, escrow.ServiceType, p.escrowID))
		}
		_ = k.Tier1Escrow.Remove(ctx, p.escrowID)
		_ = k.Tier1EscrowReleaseQueue.Remove(ctx, p.queueKey)

		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(types.NewTier1EscrowReleasedEvent(
			p.escrowID, escrow.ReportId, escrow.OperatorAddress, escrow.ServiceType,
			escrow.Amount, types.EscrowDestCommunityPool,
		))
		processed++
	}

	return processed, nil
}

// ---------------------------------------------------------------------------
// Internal helpers used by EndBlocker
// ---------------------------------------------------------------------------

// mustAddrBytesK is a panic-on-invalid wrapper for code paths that have
// already validated upstream. Mirror of mustAddrBytes from
// msg_server_resolve_report.go but for the (non-msgServer) keeper type
// — keeps the sweep code path off the msgServer embed.
func mustAddrBytesK(k Keeper, addr string) []byte {
	b, err := k.addrBytes(addr)
	if err != nil {
		panic(err)
	}
	return b
}

// findTier1EscrowForReportK is the (non-msgServer) version of
// findTier1EscrowForReport. Same logic; mirrored to avoid pulling the
// msgServer embed into endblocker.go (a more decoupled boundary makes
// it easier to test the sweeps standalone).
func (k Keeper) findTier1EscrowForReportK(
	ctx context.Context,
	opBytes []byte,
	serviceType string,
	reportID uint64,
) (types.Tier1EscrowEntry, uint64, bool, error) {
	rng := collections.NewPrefixedTripleRange[[]byte, string, uint64](opBytes)
	iter, err := k.Tier1EscrowByOperator.Iterate(ctx, rng)
	if err != nil {
		return types.Tier1EscrowEntry{}, 0, false, err
	}
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return types.Tier1EscrowEntry{}, 0, false, err
		}
		if key.K2() != serviceType {
			continue
		}
		escrowID := key.K3()
		escrow, err := k.Tier1Escrow.Get(ctx, escrowID)
		if err != nil {
			continue
		}
		if escrow.ReportId == reportID {
			return escrow, escrowID, true, nil
		}
	}
	return types.Tier1EscrowEntry{}, 0, false, nil
}
