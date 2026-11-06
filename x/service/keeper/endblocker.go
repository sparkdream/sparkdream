package keeper

import (
	"context"
	"fmt"
	"time"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
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
//  6. System-report dedup pruning (sweepSystemReportDedup).
//
// Unbond completion is NOT swept — it's lazy via
// MsgClaimUnbondedBond (§3.6).
//
// Each record runs in its own cache context (runIsolated): a record that
// fails is logged and skipped, and stays queued for the next block. A
// sweep-level error (a queue walk failing) is logged too and the other
// sweeps still run. The EndBlocker never returns an error, so a store or
// bank failure on one record cannot halt the chain (SVC-105).
func (k Keeper) EndBlocker(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	params, err := k.Params.Get(ctx)
	if err != nil {
		sdkCtx.Logger().Error("x/service EndBlocker: Params.Get failed", "err", err)
		return nil
	}
	limit := int(params.EndblockerSweepLimit)
	if limit <= 0 {
		// Defensive; Params.Validate rejects zero, but defaults to 100
		// if somehow not set.
		limit = 100
	}

	runSweep := func(name string, fn func() (int, error)) {
		start := time.Now() //nolint:forbidigo // telemetry timing only, no state depends on it
		n, err := fn()
		emitSweepDuration(name, start)
		if n > 0 {
			emitSweptRecords(name, float32(n))
		}
		if err != nil {
			sdkCtx.Logger().Error("x/service EndBlocker: sweep failed", "sweep", name, "err", err)
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
	runSweep("system_report_dedup", func() (int, error) {
		return k.sweepSystemReportDedup(ctx, params, currentHeight, limit)
	})

	return nil
}

// ---------------------------------------------------------------------------
// Sweep 1: Underfunded → forced UNBONDING (§3.6 queue 1)
// ---------------------------------------------------------------------------

// sweepUnderfunded force-unbonds UNDERFUNDED operators whose grace
// period has elapsed. It walks UnderfundedDue (keyed by grace deadline) up
// to the current height, so it visits only due entries (SVC-07): an
// operator still in grace is never read, whatever the service type's grace.
func (k Keeper) sweepUnderfunded(ctx context.Context, params types.Params, currentHeight int64, limit int) (int, error) {
	processed := 0
	if limit <= 0 {
		return 0, nil
	}
	type pending struct {
		dueKey collections.Triple[int64, []byte, string]
	}
	var batch []pending
	if err := k.UnderfundedDue.Walk(ctx, collections.NewPrefixUntilTripleRange[int64, []byte, string](currentHeight),
		func(key collections.Triple[int64, []byte, string]) (bool, error) {
			batch = append(batch, pending{dueKey: key})
			return len(batch) >= limit, nil
		}); err != nil {
		return 0, err
	}

	for _, p := range batch {
		if k.runIsolated(ctx, "sweepUnderfunded", func(ctx context.Context) (bool, error) {
			opAddr := p.dueKey.K2()
			serviceType := p.dueKey.K3()

			// Re-fetch the operator and verify it's still UNDERFUNDED — a
			// concurrent message may have transitioned it (e.g. top-up).
			op, exists := k.GetOperator(ctx, opAddr, serviceType)
			if !exists || op.Status != types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED || op.UnderfundedSince == 0 {
				// Stale entry. Remove.
				k.clearUnderfundedDue(ctx, opAddr, serviceType)
				return false, nil
			}
			underfundedSince := op.UnderfundedSince
			cfg, err := k.resolveServiceTypeConfig(ctx, op.ServiceType)
			if err != nil {
				// Service type yanked from registry mid-flight: look again
				// after the default grace instead of holding a batch slot.
				if err := k.setUnderfundedDue(ctx, opAddr, serviceType, currentHeight+k.effectiveUnderfundedGraceBlocks(types.ServiceTypeConfig{}, params)+1); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("sweepUnderfunded: k.setUnderfundedDue failed", "err", err)
				}
				return false, nil
			}
			grace := k.effectiveUnderfundedGraceBlocks(cfg, params)
			if currentHeight < underfundedSince+grace {
				// The grace grew since the entry was filed: re-file it.
				if err := k.setUnderfundedDue(ctx, opAddr, serviceType, underfundedSince+grace); err != nil {
					return false, err
				}
				return false, nil
			}

			// Force-unbond. Settle, flip status, set clock, drop the
			// UnderfundedQueue entry.
			k.settleBondBlocks(&op, currentHeight)
			op.Status = types.OperatorStatus_OPERATOR_STATUS_UNBONDING
			op.UnbondCompleteAt = currentHeight + k.effectiveUnbondingPeriodBlocks(cfg, params)
			op.UnderfundedSince = 0
			clearPendingWithdrawal(&op)

			if err := k.removeUnderfundedQueueEntry(ctx, underfundedSince, opAddr, serviceType); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("sweepUnderfunded: k.removeUnderfundedQueueEntry failed", "err", err)
			}
			if err := k.PutOperator(ctx, op); err != nil {
				return false, err
			}

			sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(types.NewOperatorUnbondStartedEvent(
				op.Address, op.ServiceType, op.UnbondCompleteAt, types.UnbondSourceForcedUnderfunded,
			))
			emitOperatorUnbondStarted(op.ServiceType, types.UnbondSourceForcedUnderfunded)
			return true, nil
		}) {
			processed++
		}
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
		if k.runIsolated(ctx, "sweepPendingReports", func(ctx context.Context) (bool, error) {
			report, err := k.Reports.Get(ctx, p.reportID)
			if err != nil {
				if err := k.PendingReportsQueue.Remove(ctx, p.queueKey); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("sweepPendingReports: PendingReportsQueue.Remove failed", "err", err)
				}
				return false, nil
			}
			if report.Status != types.ReportStatus_REPORT_STATUS_PENDING {
				if err := k.PendingReportsQueue.Remove(ctx, p.queueKey); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("sweepPendingReports: PendingReportsQueue.Remove failed", "err", err)
				}
				return false, nil
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
						return false, err
					}
					if err := k.PendingReportsQueue.Remove(ctx, p.queueKey); err != nil {
						sdk.UnwrapSDKContext(ctx).Logger().Error("sweepPendingReports: PendingReportsQueue.Remove failed", "err", err)
					}
					return true, nil
				}
				if proposedBps > cfg.UnilateralSlashCapBps {
					proposedBps = cfg.UnilateralSlashCapBps
				}
				if err := k.escalateReportToJury(ctx, &report, proposedBps, currentHeight); err != nil {
					return false, err
				}
				// escalateReportToJury already removed the PendingReportsQueue
				// entry — no further work here.
				return true, nil
			}

			// Default path: auto-dismiss.
			if err := k.timeoutDismissReport(ctx, params, &report, currentHeight); err != nil {
				return false, err
			}
			if err := k.PendingReportsQueue.Remove(ctx, p.queueKey); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("sweepPendingReports: PendingReportsQueue.Remove failed", "err", err)
			}
			return true, nil
		}) {
			processed++
		}
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
	depositCoin := sdk.NewCoin(k.BondDenom(ctx), reportDeposit(*report))
	reporterBytes, err := k.addrBytes(report.Reporter)
	if err == nil && depositCoin.IsPositive() {
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(
			ctx, types.ModuleName, sdk.AccAddress(reporterBytes), sdk.NewCoins(depositCoin),
		); err != nil {
			return err
		}
	}

	// Bar the reporter of a lapsed MEMBER report from re-filing it for
	// report_refile_cooldown_blocks. System reports (federation
	// challenges, x/service's own liveness reports) record nothing: a
	// lapsed liveness report must never stop members from reporting the
	// operator it was about.
	if !isSystemReport(*report) {
		op, exists := k.GetOperator(ctx, mustAddrBytesK(k, report.OperatorAddress), report.ServiceType)
		controller := ""
		if exists {
			controller = op.Controller
		}
		if err := k.recordRefileCooldown(ctx, controller, report.Reporter, report.OperatorAddress, report.ServiceType, currentHeight); err != nil {
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

// sweepEscalatedReports closes ESCALATED reports older than
// max_escalated_blocks that no resolver relayed. If the jury tallied
// UPHOLD, the verdict is applied as ACCEPT at the proposed slash (no
// dissolve; SVC-101). Otherwise the report becomes AUTO_TIMEOUT, treated
// as REJECT-equivalent (no slash; contested-T1 escrow returns to bond;
// reporter/opener deposit refunded; jury case canceled). See §3.6 +
// §3.4.5.
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
		if k.runIsolated(ctx, "sweepEscalatedReports", func(ctx context.Context) (bool, error) {
			report, err := k.Reports.Get(ctx, p.reportID)
			if err != nil {
				if err := k.EscalatedReportsQueue.Remove(ctx, p.queueKey); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("sweepEscalatedReports: EscalatedReportsQueue.Remove failed", "err", err)
				}
				return false, nil
			}
			if report.Status != types.ReportStatus_REPORT_STATUS_ESCALATED {
				if err := k.EscalatedReportsQueue.Remove(ctx, p.queueKey); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("sweepEscalatedReports: EscalatedReportsQueue.Remove failed", "err", err)
				}
				return false, nil
			}

			// SVC-101: a jury that upheld the report decides the outcome
			// even when no resolver relayed it. Apply the proposed slash
			// (ACCEPT, no dissolve) instead of timing the report out.
			if k.juryUpheld(ctx, report.JuryCaseId) {
				if err := k.applyJuryVerdict(ctx, report, types.JuryVerdict_JURY_VERDICT_ACCEPT, report.ProposedSlashBps, false); err != nil {
					return false, err
				}
				return true, nil
			}

			opBytes, err := k.addrBytes(report.OperatorAddress)
			if err != nil {
				return false, nil
			}

			// Release any contested-T1 escrow back to the operator's bond.
			returned := sdkmath.ZeroInt()
			op, exists := k.GetOperator(ctx, opBytes, report.ServiceType)
			if exists {
				escrow, escrowID, hasEscrow, err := k.findTier1EscrowForReportK(ctx, opBytes, report.ServiceType, report.ReportId)
				if err == nil && hasEscrow {
					returned = escrow.Amount
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
									if err := k.removeUnderfundedQueueEntry(ctx, oldUF, opBytes, op.ServiceType); err != nil {
										sdk.UnwrapSDKContext(ctx).Logger().Error("sweepEscalatedReports: k.removeUnderfundedQueueEntry failed", "err", err)
									}
								}
								refundedTransition = true
							}
						}
					}
					if err := k.PutOperator(ctx, op); err != nil {
						return false, err
					}
					if refundedTransition && k.hooks() != nil {
						k.hooks().AfterOperatorReFunded(ctx, sdk.AccAddress(opBytes), op.ServiceType)
					}
					if err := k.Tier1Escrow.Remove(ctx, escrowID); err != nil {
						sdk.UnwrapSDKContext(ctx).Logger().Error("sweepEscalatedReports: Tier1Escrow.Remove failed", "err", err)
					}
					if err := k.Tier1EscrowByOperator.Remove(ctx, collections.Join3(opBytes, op.ServiceType, escrowID)); err != nil {
						sdk.UnwrapSDKContext(ctx).Logger().Error("sweepEscalatedReports: Tier1EscrowByOperator.Remove failed", "err", err)
					}
					if err := k.Tier1EscrowReleaseQueue.Remove(ctx, collections.Join(escrow.ReleaseAt, escrowID)); err != nil {
						sdk.UnwrapSDKContext(ctx).Logger().Error("sweepEscalatedReports: Tier1EscrowReleaseQueue.Remove failed", "err", err)
					}
				}
			}

			// Refund reporter deposit (AUTO_TIMEOUT is treated as REJECT-
			// equivalent for the reporter — they're not at fault for the
			// jury not deciding).
			bondDenom := k.BondDenom(ctx)
			// A nil deposit (pre-field record) is a zero deposit; NewCoin
			// would panic on it.
			depositCoin := sdk.NewCoin(bondDenom, reportDeposit(report))
			if reporterBytes, err := k.addrBytes(report.Reporter); err == nil && depositCoin.IsPositive() {
				if err := k.bankKeeper.SendCoinsFromModuleToAccount(
					ctx, types.ModuleName, sdk.AccAddress(reporterBytes), sdk.NewCoins(depositCoin),
				); err != nil {
					return false, err
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
				return false, err
			}
			if err := k.EscalatedReportsQueue.Remove(ctx, p.queueKey); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("sweepEscalatedReports: EscalatedReportsQueue.Remove failed", "err", err)
			}

			// AUTO_TIMEOUT is treated as REJECT-equivalent. Any contested-T1
			// escrow still held was returned to bond by the inner block above
			// and is reported here. (A contest returns the escrow before
			// escalation, so this is zero on every path reachable today.)
			sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
				types.NewReportAutoTimeoutEvent(report.ReportId, depositCoin, sdk.NewCoin(bondDenom, returned)),
			)
			return true, nil
		}) {
			processed++
		}
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
// The sweep reads ControllerTransferCasesByOpenedAt in opened_at order and
// stops at the first case that is not due yet, so it never reads undue
// cases (every case shares max_escalated_blocks, so opened_at order is due
// order). At most `limit` cases are timed out per block; the rest go next
// block.
func (k Keeper) sweepControllerTransferCases(ctx context.Context, params types.Params, currentHeight int64, limit int) (int, error) {
	var expired []types.ControllerTransferCase
	var stale []collections.Pair[int64, uint64]
	err := k.ControllerTransferCasesByOpenedAt.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
		if currentHeight < key.K1()+params.MaxEscalatedBlocks {
			return true, nil
		}
		c, err := k.ControllerTransferCases.Get(ctx, key.K2())
		if err != nil {
			// The case is gone; drop its index entry.
			stale = append(stale, key)
			return false, nil
		}
		expired = append(expired, c)
		return len(expired) >= limit, nil
	})
	if err != nil {
		return 0, err
	}
	for _, key := range stale {
		if err := k.ControllerTransferCasesByOpenedAt.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("sweepControllerTransferCases: ControllerTransferCasesByOpenedAt.Remove failed", "err", err)
		}
	}

	processed := 0
	for _, c := range expired {
		if k.runIsolated(ctx, "sweepControllerTransferCases", func(ctx context.Context) (bool, error) {
			sdkCtx := sdk.UnwrapSDKContext(ctx)
			if !c.Deposit.IsNil() && c.Deposit.IsPositive() {
				openerBytes, err := k.addrBytes(c.Opener)
				if err != nil {
					return false, err
				}
				depositCoin := sdk.NewCoin(k.BondDenom(ctx), c.Deposit)
				if err := k.bankKeeper.SendCoinsFromModuleToAccount(
					ctx, types.ModuleName, sdk.AccAddress(openerBytes), sdk.NewCoins(depositCoin),
				); err != nil {
					return false, err
				}
			}

			if err := k.removeControllerTransferCase(ctx, c); err != nil {
				return false, err
			}
			if opBytes, err := k.addrBytes(c.OperatorAddress); err == nil {
				idxKey := collections.Join(opBytes, c.ServiceType)
				// Only drop the index if it still points at this case.
				if id, err := k.OpenControllerTransferByOperator.Get(ctx, idxKey); err == nil && id == c.JuryCaseId {
					if err := k.OpenControllerTransferByOperator.Remove(ctx, idxKey); err != nil {
						sdk.UnwrapSDKContext(ctx).Logger().Error("sweepControllerTransferCases: OpenControllerTransferByOperator.Remove failed", "err", err)
					}
				}
			}

			sdkCtx.EventManager().EmitEvent(types.NewControllerTransferCaseFinalizedEvent(
				c.JuryCaseId, types.CtfVerdictAutoTimeout, types.CtfDepositOpenerRefunded,
			))
			emitControllerTransferCaseFinalized(c.ServiceType, types.CtfVerdictAutoTimeout)
			return true, nil
		}) {
			processed++
		}
	}
	return processed, nil
}

// ---------------------------------------------------------------------------
// Sweep 4: Tier-1 escrow release (§3.6 queue 4)
// ---------------------------------------------------------------------------

// releaseTier1Escrow finalizes one matured tier-1 escrow: the SPARK goes to
// the community pool, the §6.6 reputation deduction is applied against the
// bond as it stood before the slash, and the escrow and its indexes are
// removed. Both the EndBlocker sweep and the claim paths
// (eagerReleaseExpiredEscrowsForOperator) finalize through it, so whichever
// runs first applies the same accounting; a claim can no longer finalize the
// slash without the deduction.
func (k Keeper) releaseTier1Escrow(ctx context.Context, escrowID uint64, escrow types.Tier1EscrowEntry, currentHeight int64) error {
	escrowCoin := sdk.NewCoin(k.BondDenom(ctx), escrow.Amount)
	if k.distributionKeeper() != nil && !escrow.Amount.IsZero() {
		if err := k.distributionKeeper().FundCommunityPool(
			ctx, sdk.NewCoins(escrowCoin), k.bankModuleAddress(),
		); err != nil {
			return err
		}
	}

	opBytes, err := k.addrBytes(escrow.OperatorAddress)
	if err == nil {
		if err := k.Tier1EscrowByOperator.Remove(ctx, collections.Join3(opBytes, escrow.ServiceType, escrowID)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("releaseTier1Escrow: Tier1EscrowByOperator.Remove failed", "err", err)
		}

		// The tier-1 slash is final now (uncontested through the window):
		// apply the reputation deduction. A dissolved operator has no live
		// record and gets no deduction.
		if op, live := k.GetOperator(ctx, opBytes, escrow.ServiceType); live {
			// Settle on a local copy so the deduction sees accrual up to
			// this height; the stored record is left for the next mutation
			// to settle from its own timestamp.
			k.settleBondBlocks(&op, currentHeight)
			bondBefore := op.BondAmount.Add(escrow.Amount)
			if err := k.deductReputationOnSlash(ctx, op, opBytes, escrow.Amount, bondBefore, 1); err != nil {
				// Best effort: an x/rep failure must not trap the bond.
				sdk.UnwrapSDKContext(ctx).Logger().Warn(
					"x/service: reputation deduction on tier-1 release failed (non-fatal)",
					"operator", escrow.OperatorAddress,
					"service_type", escrow.ServiceType,
					"error", err,
				)
			}
		}
	}
	if err := k.Tier1Escrow.Remove(ctx, escrowID); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("releaseTier1Escrow: Tier1Escrow.Remove failed", "err", err)
	}
	if err := k.Tier1EscrowReleaseQueue.Remove(ctx, collections.Join(escrow.ReleaseAt, escrowID)); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("releaseTier1Escrow: Tier1EscrowReleaseQueue.Remove failed", "err", err)
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(types.NewTier1EscrowReleasedEvent(
		escrowID, escrow.ReportId, escrow.OperatorAddress, escrow.ServiceType,
		escrowCoin, types.EscrowDestCommunityPool,
	))
	return nil
}

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
		if k.runIsolated(ctx, "sweepTier1EscrowRelease", func(ctx context.Context) (bool, error) {
			escrow, err := k.Tier1Escrow.Get(ctx, p.escrowID)
			if err != nil {
				if err := k.Tier1EscrowReleaseQueue.Remove(ctx, p.queueKey); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("sweepTier1EscrowRelease: Tier1EscrowReleaseQueue.Remove failed", "err", err)
				}
				return false, nil
			}

			// Parent report must be in a terminal state. If still
			// PENDING/ESCALATED/RESOLVED_T1 within window, skip — that
			// shouldn't happen given release_at, but defensive.
			report, err := k.Reports.Get(ctx, escrow.ReportId)
			if err != nil {
				if err := k.Tier1EscrowReleaseQueue.Remove(ctx, p.queueKey); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("sweepTier1EscrowRelease: Tier1EscrowReleaseQueue.Remove failed", "err", err)
				}
				return false, nil
			}
			// AUTO_TIMEOUT already returned funds to bond via sweep 3 (above)
			// — defensive guard.
			if report.Status == types.ReportStatus_REPORT_STATUS_PENDING ||
				report.Status == types.ReportStatus_REPORT_STATUS_ESCALATED {
				return false, nil
			}

			if err := k.releaseTier1Escrow(ctx, p.escrowID, escrow, currentHeight); err != nil {
				return false, err
			}
			if err := k.Tier1EscrowReleaseQueue.Remove(ctx, p.queueKey); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("sweepTier1EscrowRelease: Tier1EscrowReleaseQueue.Remove failed", "err", err)
			}
			return true, nil
		}) {
			processed++
		}
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

// runIsolated runs one EndBlocker item in a cache context, so its writes
// and events land only if it succeeds. A failing item is logged and skipped
// (it stays queued and is retried next block) instead of failing
// FinalizeBlock and halting the chain (SVC-105). A panic inside the item
// (a nil late-wired keeper, a negative coin, a rep-side panic while seating
// a jury) is recovered the same way (SVC-202); only out-of-gas is re-raised,
// as in federation's hook isolation, because the block gas meter owns that
// decision. fn reports whether the item counts as processed; the return is
// that flag, false on error or panic.
func (k Keeper) runIsolated(ctx context.Context, sweep string, fn func(ctx context.Context) (bool, error)) (counted bool) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	cacheCtx, write := sdkCtx.CacheContext()
	defer func() {
		if r := recover(); r != nil {
			if _, oog := r.(storetypes.ErrorOutOfGas); oog {
				panic(r)
			}
			sdkCtx.Logger().Error("x/service EndBlocker: item panicked and was skipped", "sweep", sweep, "panic", fmt.Sprintf("%v", r))
			counted = false
		}
	}()
	counted, err := fn(cacheCtx)
	if err != nil {
		sdkCtx.Logger().Error("x/service EndBlocker: item skipped", "sweep", sweep, "err", err)
		return false
	}
	write()
	return counted
}

// juryUpheld reports whether the x/rep JuryReview behind a report tallied
// UPHOLD. False when x/rep is unwired, the report has no jury case, or the
// review cannot be read.
func (k Keeper) juryUpheld(ctx context.Context, juryCaseID uint64) bool {
	if k.repKeeper() == nil || juryCaseID == 0 {
		return false
	}
	name, err := k.repKeeper().GetJuryVerdictName(ctx, juryCaseID)
	return err == nil && name == types.JuryVerdictUpholdChallenge
}
