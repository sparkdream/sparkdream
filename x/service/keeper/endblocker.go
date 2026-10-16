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
//  3. Escalated report auto-timeout (jury inaction), and its
//     controller-transfer twin: cases still open past
//     opened_at + max_escalated_blocks time out (see
//     sweepControllerTransferCases).
//  4. Tier-1 escrow release (post-contest-window funds → community pool).
//  5. Checkpoint liveness (operators whose checkpoint trails the chain by
//     more than their type's checkpoint_max_lag_blocks get a system
//     report; see checkpoints.go).
//
// Unbond completion is NOT swept — it's lazy via
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
	runSweep("controller_transfer_cases", func() (int, error) {
		return k.sweepControllerTransferCases(ctx, params, currentHeight, limit)
	})
	runSweep("tier1_escrow_release", func() (int, error) {
		return k.sweepTier1EscrowRelease(ctx, currentHeight, limit)
	})
	runSweep("checkpoint_liveness", func() (int, error) {
		return k.sweepCheckpointLiveness(ctx, currentHeight, limit)
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
		clearPendingWithdrawal(&op)

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

		// Look up the report's ServiceTypeConfig to decide whether this
		// timeout dismisses or escalates (Phase 0 of the federation→
		// service migration plan).
		cfg, cfgErr := k.resolveServiceTypeConfig(ctx, report.ServiceType)
		shouldEscalate := cfgErr == nil &&
			cfg.ReportTimeoutAction == types.ReportTimeoutAction_REPORT_TIMEOUT_ACTION_ESCALATE

		if shouldEscalate {
			// Fall back to the configured default slash if the report
			// has no proposal yet (member-filed reports start with 0).
			// System reports filed via OpenSystemReport already carry
			// the appropriate ProposedSlashBps.
			proposedBps := report.ProposedSlashBps
			if proposedBps == 0 {
				proposedBps = cfg.ChallengeDefaultSlashBps
			}
			if proposedBps == 0 {
				// No usable slash proposal — fall back to dismissal so we
				// don't open a jury case the jurors can't meaningfully
				// vote on.
				if err := k.timeoutDismissReport(ctx, params, &report, currentHeight); err != nil {
					return processed, err
				}
				_ = k.PendingReportsQueue.Remove(ctx, p.queueKey)
				processed++
				continue
			}
			if proposedBps > cfg.UnilateralSlashCapBps {
				proposedBps = cfg.UnilateralSlashCapBps
			}
			if err := k.escalateReportToJury(ctx, &report, proposedBps, currentHeight); err != nil {
				return processed, err
			}
			// escalateReportToJury already removed the PendingReportsQueue
			// entry — no further work here.
			processed++
			continue
		}

		// Default path: auto-dismiss.
		if err := k.timeoutDismissReport(ctx, params, &report, currentHeight); err != nil {
			return processed, err
		}
		_ = k.PendingReportsQueue.Remove(ctx, p.queueKey)
		processed++
	}

	return processed, nil
}

// timeoutDismissReport executes the AUTO_DISMISSED path: refund deposit,
// record refile cooldown, flip status, emit event. Caller is
// responsible for removing the PendingReportsQueue entry.
func (k Keeper) timeoutDismissReport(
	ctx context.Context,
	params types.Params,
	report *types.Report,
	currentHeight int64,
) error {
	// Refund deposit to reporter (zero for system reports, positive
	// for member-filed reports).
	depositCoin := sdk.NewCoin(k.BondDenom(ctx), report.Deposit)
	reporterBytes, err := k.addrBytes(report.Reporter)
	if err == nil && !report.Deposit.IsNil() && !report.Deposit.IsZero() {
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(
			ctx, types.ModuleName, sdk.AccAddress(reporterBytes), sdk.NewCoins(depositCoin),
		); err != nil {
			return err
		}
	}

	// Record refile cooldown against the operator's current controller.
	op, exists := k.GetOperator(ctx, mustAddrBytesK(k, report.OperatorAddress), report.ServiceType)
	if exists {
		if err := k.recordRefileCooldown(ctx, op.Controller, report.OperatorAddress, report.ServiceType, currentHeight); err != nil {
			return err
		}
	}

	report.Status = types.ReportStatus_REPORT_STATUS_AUTO_DISMISSED
	if err := k.Reports.Set(ctx, report.ReportId, *report); err != nil {
		return err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		types.NewReportAutoDismissedEvent(report.ReportId, depositCoin),
	)
	_ = params
	return nil
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
				op.BondAmount = op.BondAmount.Add(escrow.Amount)
				if !op.Tier1SlashedInWindow.IsNil() && !escrow.Amount.IsZero() {
					op.Tier1SlashedInWindow = op.Tier1SlashedInWindow.Sub(escrow.Amount)
					if op.Tier1SlashedInWindow.IsNegative() {
						op.Tier1SlashedInWindow = sdkmath.ZeroInt()
					}
				}
				// Status may revert UNDERFUNDED → ACTIVE if bond clears
				// min_bond again.
				refundedTransition := false
				if op.Status == types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED {
					if cfg, err := k.resolveServiceTypeConfig(ctx, op.ServiceType); err == nil {
						if op.BondAmount.GTE(cfg.MinBondAmount) {
							oldUF := op.UnderfundedSince
							op.Status = types.OperatorStatus_OPERATOR_STATUS_ACTIVE
							op.UnderfundedSince = 0
							if oldUF > 0 {
								_ = k.removeUnderfundedQueueEntry(ctx, oldUF, opBytes, op.ServiceType)
							}
							refundedTransition = true
						}
					}
				}
				if err := k.PutOperator(ctx, op); err != nil {
					return processed, err
				}
				if refundedTransition && k.hooks() != nil {
					k.hooks().AfterOperatorReFunded(ctx, sdk.AccAddress(opBytes), op.ServiceType)
				}
				_ = k.Tier1Escrow.Remove(ctx, escrowID)
				_ = k.Tier1EscrowByOperator.Remove(ctx, collections.Join3(opBytes, op.ServiceType, escrowID))
				_ = k.Tier1EscrowReleaseQueue.Remove(ctx, collections.Join(escrow.ReleaseAt, escrowID))
			}
		}

		// Refund reporter deposit (AUTO_TIMEOUT is treated as REJECT-
		// equivalent for the reporter — they're not at fault for the
		// jury not deciding).
		bondDenom := k.BondDenom(ctx)
		depositCoin := sdk.NewCoin(bondDenom, report.Deposit)
		if reporterBytes, err := k.addrBytes(report.Reporter); err == nil && !report.Deposit.IsZero() {
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(
				ctx, types.ModuleName, sdk.AccAddress(reporterBytes), sdk.NewCoins(depositCoin),
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
			types.NewReportAutoTimeoutEvent(report.ReportId, depositCoin, sdk.NewCoin(bondDenom, sdkmath.ZeroInt())),
		)
		processed++
	}

	return processed, nil
}

// sweepControllerTransferCases times out controller-transfer cases still
// open at opened_at + max_escalated_blocks — the same deadline
// OpenControllerTransferCase hands x/rep for the jury case. Without it a
// case whose jury never reaches a verdict (FinalizeControllerTransfer
// refuses a PENDING JuryReview) would hold the (operator, service_type)
// "one open case" slot forever.
//
// AUTO_TIMEOUT mirrors sweep 3 for reports: the opener's deposit is
// refunded (the case went unadjudicated through no fault of theirs), the
// controller is unchanged, and the case row plus its
// OpenControllerTransferByOperator entry are deleted. The parallel x/rep
// JuryReview is left in place; a late FinalizeControllerTransfer against
// it fails with ErrControllerTransferCaseNotFound.
//
// There is no deadline index, so this walks ControllerTransferCases in
// jury_case_id order. The map holds at most one case per (operator,
// service_type), and rows written before this sweep existed are covered
// by the same walk, so no migration is needed. At most `limit` cases are
// timed out per block; the rest go next block.
func (k Keeper) sweepControllerTransferCases(ctx context.Context, params types.Params, currentHeight int64, limit int) (int, error) {
	var expired []types.ControllerTransferCase
	err := k.ControllerTransferCases.Walk(ctx, nil, func(_ uint64, c types.ControllerTransferCase) (bool, error) {
		if currentHeight < c.OpenedAt+params.MaxEscalatedBlocks {
			return false, nil
		}
		expired = append(expired, c)
		return len(expired) >= limit, nil
	})
	if err != nil {
		return 0, err
	}

	processed := 0
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	for _, c := range expired {
		if !c.Deposit.IsNil() && c.Deposit.IsPositive() {
			openerBytes, err := k.addrBytes(c.Opener)
			if err != nil {
				return processed, err
			}
			depositCoin := sdk.NewCoin(k.BondDenom(ctx), c.Deposit)
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(
				ctx, types.ModuleName, sdk.AccAddress(openerBytes), sdk.NewCoins(depositCoin),
			); err != nil {
				return processed, err
			}
		}

		if err := k.ControllerTransferCases.Remove(ctx, c.JuryCaseId); err != nil {
			return processed, err
		}
		if opBytes, err := k.addrBytes(c.OperatorAddress); err == nil {
			idxKey := collections.Join(opBytes, c.ServiceType)
			// Only drop the index if it still points at this case.
			if id, err := k.OpenControllerTransferByOperator.Get(ctx, idxKey); err == nil && id == c.JuryCaseId {
				_ = k.OpenControllerTransferByOperator.Remove(ctx, idxKey)
			}
		}

		sdkCtx.EventManager().EmitEvent(types.NewControllerTransferCaseFinalizedEvent(
			c.JuryCaseId, types.CtfVerdictAutoTimeout, types.CtfDepositOpenerRefunded,
		))
		emitControllerTransferCaseFinalized(c.ServiceType, types.CtfVerdictAutoTimeout)
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
		escrowCoin := sdk.NewCoin(k.BondDenom(ctx), escrow.Amount)
		if k.distributionKeeper() != nil && !escrow.Amount.IsZero() {
			if err := k.distributionKeeper().FundCommunityPool(
				ctx, sdk.NewCoins(escrowCoin), k.bankModuleAddress(),
			); err != nil {
				return processed, err
			}
		}

		opBytes, err := k.addrBytes(escrow.OperatorAddress)
		if err == nil {
			_ = k.Tier1EscrowByOperator.Remove(ctx, collections.Join3(opBytes, escrow.ServiceType, p.escrowID))

			// The tier-1 slash is final now (uncontested through the
			// window): apply the §6.6 reputation deduction against the
			// bond as it stood before the slash. A dissolved operator has
			// no live record and gets no deduction.
			if op, live := k.GetOperator(ctx, opBytes, escrow.ServiceType); live {
				// Settle on a local copy so the deduction sees accrual up to
				// this height; the stored record is left for the next mutation
				// to settle from its own timestamp.
				k.settleBondBlocks(&op, currentHeight)
				bondBefore := op.BondAmount.Add(escrow.Amount)
				if err := k.deductReputationOnSlash(ctx, op, opBytes, escrow.Amount, bondBefore, 1); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Warn(
						"x/service: reputation deduction on tier-1 release failed (non-fatal)",
						"operator", escrow.OperatorAddress,
						"service_type", escrow.ServiceType,
						"error", err,
					)
				}
			}
		}
		_ = k.Tier1Escrow.Remove(ctx, p.escrowID)
		_ = k.Tier1EscrowReleaseQueue.Remove(ctx, p.queueKey)

		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(types.NewTier1EscrowReleasedEvent(
			p.escrowID, escrow.ReportId, escrow.OperatorAddress, escrow.ServiceType,
			escrowCoin, types.EscrowDestCommunityPool,
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
