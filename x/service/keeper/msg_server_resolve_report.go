package keeper

import (
	"bytes"
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/service/types"
)

// ResolveReport is the controller's verdict on a PENDING report. See
// x-service-spec.md §5.2. Three verdicts:
//
//   - T1_DISMISS: closes the report without slash; reporter's deposit
//     refunded.
//   - T1_SLASH: slashes the operator's bond up to unilateral_slash_cap_bps;
//     moved to Tier1Escrow pending the contest window; reporter's deposit
//     refunded.
//   - ESCALATE_TO_JURY: stores the controller's proposed_slash_bps on the
//     report, transitions to ESCALATED, and opens an x/rep jury case via
//     RepKeeper.CreateAppealInitiative (the same generic-jury entry-point
//     used by forum's moderation appeals and rep's gov_action_appeal —
//     §6.2).
//
// On all three paths the PENDING-queue index entry is removed.
func (k msgServer) ResolveReport(ctx context.Context, msg *types.MsgResolveReport) (*types.MsgResolveReportResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	controllerBytes, err := k.addrBytes(msg.Controller)
	if err != nil {
		return nil, types.ErrUnauthorizedController.Wrap("invalid controller address")
	}

	report, err := k.Reports.Get(ctx, msg.ReportId)
	if err != nil {
		return nil, types.ErrReportNotFound.Wrap(err.Error())
	}
	if report.Status != types.ReportStatus_REPORT_STATUS_PENDING {
		return nil, types.ErrReportAlreadyResolved.Wrap(report.Status.String())
	}

	op, exists := k.GetOperator(ctx, mustAddrBytes(k, report.OperatorAddress), report.ServiceType)
	if !exists {
		// Operator has been archived since the report was filed — this
		// is the §3.4.9 dissolution-closed path, which should have been
		// taken when the operator was SLASHED. Defensive guard: report
		// must already be CLOSED_OPERATOR_DISSOLVED, but we hit here
		// only if the close logic missed this report. Reject so the
		// controller doesn't try to slash a non-existent operator.
		return nil, types.ErrOperatorNotFound.Wrap("operator archived; report should have been closed")
	}

	// Signer must equal the operator's controller.
	expectedControllerBytes, err := k.addrBytes(op.Controller)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(controllerBytes, expectedControllerBytes) {
		return nil, types.ErrUnauthorizedController.Wrapf("expected %s, got %s", op.Controller, msg.Controller)
	}

	cfg, err := k.resolveServiceTypeConfig(ctx, op.ServiceType)
	if err != nil {
		return nil, err
	}

	switch msg.Verdict {
	case types.ResolveVerdict_RESOLVE_VERDICT_T1_DISMISS:
		if msg.SlashBps != 0 {
			return nil, types.ErrInvalidVerdict.Wrap("T1_DISMISS requires slash_bps == 0")
		}
		return k.handleT1Dismiss(ctx, &report, currentHeight)

	case types.ResolveVerdict_RESOLVE_VERDICT_T1_SLASH:
		if msg.SlashBps == 0 {
			return nil, types.ErrInvalidVerdict.Wrap("T1_SLASH requires slash_bps > 0")
		}
		if msg.SlashBps > cfg.UnilateralSlashCapBps {
			return nil, types.ErrSlashCapExceeded.Wrapf(
				"slash_bps %d > cap %d (use ESCALATE_TO_JURY for larger)",
				msg.SlashBps, cfg.UnilateralSlashCapBps,
			)
		}
		return k.handleT1Slash(ctx, &report, &op, cfg, msg.SlashBps, currentHeight)

	case types.ResolveVerdict_RESOLVE_VERDICT_ESCALATE_TO_JURY:
		if msg.SlashBps == 0 {
			return nil, types.ErrInvalidVerdict.Wrap("ESCALATE_TO_JURY requires slash_bps > 0 (proposed slash)")
		}
		return k.handleEscalateToJury(ctx, &report, msg.SlashBps, currentHeight)

	default:
		return nil, types.ErrInvalidVerdict.Wrap(msg.Verdict.String())
	}
}

// handleT1Dismiss closes a PENDING report without slash; reporter's
// deposit is refunded. No state change to the operator.
func (k msgServer) handleT1Dismiss(
	ctx context.Context,
	report *types.Report,
	currentHeight int64,
) (*types.MsgResolveReportResponse, error) {
	report.Status = types.ReportStatus_REPORT_STATUS_RESOLVED_T1
	// slash_amount already zero from filing.

	if err := k.refundReportDeposit(ctx, report); err != nil {
		return nil, err
	}

	if err := k.persistReportClosure(ctx, report, currentHeight); err != nil {
		return nil, err
	}

	bondDenom := k.BondDenom(ctx)
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		types.NewReportResolvedT1Event(report.ReportId, "DISMISS", sdk.NewCoin(bondDenom, report.SlashAmount)),
	)
	emitReportResolved(report.ServiceType, types.TierTier1, "DISMISS")
	return &types.MsgResolveReportResponse{}, nil
}

// handleT1Slash applies a tier-1 slash:
//   - cooldown check (§3.4.4),
//   - aggregate-cap check + window-roll (§3.4.3),
//   - settle bond-blocks BEFORE bond mutation (§6.6),
//   - move slash amount from bond → Tier1Escrow (§3.4.7),
//   - record Tier1LastSlash,
//   - flip status to UNDERFUNDED if new bond < min_bond,
//   - reporter deposit refunded,
//   - report status → RESOLVED_T1 (contestable for report_contest_window_blocks).
//
// Note: bond → 0 via T1_SLASH does NOT auto-archive (the escrow could be
// contested and returned). Archival on zero-bond happens on T2 ACCEPT or
// dissolve=true.
func (k msgServer) handleT1Slash(
	ctx context.Context,
	report *types.Report,
	op *types.Operator,
	cfg types.ServiceTypeConfig,
	slashBps uint32,
	currentHeight int64,
) (*types.MsgResolveReportResponse, error) {
	opBytes, err := k.addrBytes(op.Address)
	if err != nil {
		return nil, err
	}
	controllerBytes, err := k.addrBytes(op.Controller)
	if err != nil {
		return nil, err
	}

	// Cooldown gate.
	if err := k.checkTier1Cooldown(ctx, cfg, controllerBytes, opBytes, op.ServiceType, currentHeight); err != nil {
		return nil, err
	}

	// Compute slash on CURRENT bond (§3.4.2 — basis points of current).
	slashAmount := computeSlashAmount(op.BondAmount, slashBps)
	if slashAmount.IsZero() {
		// Defensive: with current_bond=0 or bps=0, no slash possible —
		// surface as a controller-friendly error rather than silent
		// no-op.
		return nil, types.ErrInsufficientBond.Wrap("nothing to slash (bond is zero)")
	}

	// Aggregate-cap gate (also rolls the window if expired).
	if err := k.checkAndAdvanceTier1Window(op, cfg, slashAmount, currentHeight); err != nil {
		return nil, err
	}

	// Settle bond-blocks BEFORE the bond mutation (§6.6).
	k.settleBondBlocks(op, currentHeight)

	// Apply slash (debit bond + flip to UNDERFUNDED if needed).
	k.applySlashToBond(op, cfg, slashAmount, currentHeight)
	op.Tier1SlashedInWindow = op.Tier1SlashedInWindow.Add(slashAmount)

	// Tier1Escrow row + indexes.
	escrowID, err := k.NextEscrowID.Next(ctx)
	if err != nil {
		return nil, err
	}
	if escrowID == 0 {
		escrowID, err = k.NextEscrowID.Next(ctx)
		if err != nil {
			return nil, err
		}
	}
	// release_at uses the MODULE param (not the per-type one). Spec
	// §3.4.7 ties it to `report_contest_window_blocks` (global).
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	releaseAt := currentHeight + params.ReportContestWindowBlocks

	bondDenom := k.BondDenom(ctx)
	escrow := types.Tier1EscrowEntry{
		EscrowId:        escrowID,
		ReportId:        report.ReportId,
		OperatorAddress: op.Address,
		ServiceType:     op.ServiceType,
		Amount:          slashAmount,
		ReleaseAt:       releaseAt,
	}
	if err := k.Tier1Escrow.Set(ctx, escrowID, escrow); err != nil {
		return nil, err
	}
	if err := k.Tier1EscrowByOperator.Set(ctx, collections.Join3(opBytes, op.ServiceType, escrowID)); err != nil {
		return nil, err
	}
	if err := k.Tier1EscrowReleaseQueue.Set(ctx, collections.Join(releaseAt, escrowID)); err != nil {
		return nil, err
	}

	// Tier1LastSlash for cooldown enforcement.
	if err := k.recordTier1Slash(ctx, op.Controller, op.Address, op.ServiceType, currentHeight); err != nil {
		return nil, err
	}

	// Persist operator. The status may have flipped ACTIVE→UNDERFUNDED
	// inside applySlashToBond; PutOperator's index logic adds the
	// UnderfundedQueue entry based on the current status.
	if err := k.PutOperator(ctx, *op); err != nil {
		return nil, err
	}

	// Update report: status RESOLVED_T1, slash_amount, proposed_slash_bps.
	report.Status = types.ReportStatus_REPORT_STATUS_RESOLVED_T1
	report.SlashAmount = slashAmount
	report.ProposedSlashBps = slashBps

	// Refund reporter's deposit (slash > 0 → refund per §3.4.6).
	if err := k.refundReportDeposit(ctx, report); err != nil {
		return nil, err
	}

	if err := k.persistReportClosure(ctx, report, currentHeight); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	emitOperatorUnderfunded := op.Status == types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED && op.UnderfundedSince == currentHeight
	slashCoin := sdk.NewCoin(bondDenom, slashAmount)
	bondCoin := sdk.NewCoin(bondDenom, op.BondAmount)
	events := sdk.Events{
		types.NewOperatorSlashedEvent(op.Address, op.ServiceType, slashCoin, bondCoin, types.TierTier1, report.ReportId),
		types.NewTier1EscrowLockedEvent(escrowID, report.ReportId, op.Address, op.ServiceType, slashCoin, releaseAt),
		types.NewReportResolvedT1Event(report.ReportId, "SLASH", slashCoin),
	}
	if emitOperatorUnderfunded {
		cfg, _ := k.resolveServiceTypeConfig(ctx, op.ServiceType)
		events = append(events, types.NewOperatorUnderfundedEvent(
			op.Address, op.ServiceType, bondCoin, sdk.NewCoin(bondDenom, cfg.MinBondAmount), currentHeight+k.effectiveUnderfundedGraceBlocks(cfg, params),
		))
	}
	sdkCtx.EventManager().EmitEvents(events)
	emitReportResolved(report.ServiceType, types.TierTier1, "SLASH")
	emitSlashAmount(op.ServiceType, types.TierTier1, float32(slashAmount.Int64()))

	// AfterOperatorUnderfunded fires AFTER state writes so consumer
	// hooks iterating live indexes see the post-transition state. Skip
	// if already in UNBONDING (operator is exiting anyway).
	if emitOperatorUnderfunded && k.hooks() != nil {
		k.hooks().AfterOperatorUnderfunded(ctx, sdk.AccAddress(opBytes), op.ServiceType)
	}

	return &types.MsgResolveReportResponse{}, nil
}

// handleEscalateToJury transitions PENDING → ESCALATED with the
// controller's proposed_slash_bps recorded. Thin msg-server wrapper
// around the keeper-level escalateReportToJury so EndBlocker (Phase 0
// auto-escalate-on-timeout) can share the logic.
func (k msgServer) handleEscalateToJury(
	ctx context.Context,
	report *types.Report,
	proposedSlashBps uint32,
	currentHeight int64,
) (*types.MsgResolveReportResponse, error) {
	if err := k.Keeper.escalateReportToJury(ctx, report, proposedSlashBps, currentHeight); err != nil {
		return nil, err
	}
	return &types.MsgResolveReportResponse{}, nil
}

// escalateReportToJury is the keeper-level escalation primitive. Used
// both by MsgResolveReport(ESCALATE_TO_JURY) and by the EndBlocker
// timeout sweep when the report's ServiceTypeConfig has
// ReportTimeoutAction=ESCALATE.
//
// Performs:
//   - PENDING → ESCALATED state transition;
//   - Opens an x/rep jury case via CreateAppealInitiative (skipped if
//     repKeeper is nil, e.g. in standalone-mode tests);
//   - Moves the report from PendingReportsQueue to EscalatedReportsQueue;
//   - Emits service.report_escalated.
func (k Keeper) escalateReportToJury(
	ctx context.Context,
	report *types.Report,
	proposedSlashBps uint32,
	currentHeight int64,
) error {
	report.Status = types.ReportStatus_REPORT_STATUS_ESCALATED
	report.ProposedSlashBps = proposedSlashBps
	report.EscalatedAt = currentHeight

	if k.repKeeper() != nil {
		params, err := k.Params.Get(ctx)
		if err != nil {
			return err
		}
		payload := []byte(fmt.Sprintf(
			`{"report_id":%d,"operator":%q,"service_type":%q,"reporter":%q,"reason":%q,"proposed_slash_bps":%d}`,
			report.ReportId, report.OperatorAddress, report.ServiceType,
			report.Reporter, report.Reason, proposedSlashBps,
		))
		deadline := currentHeight + params.MaxEscalatedBlocks
		caseID, err := k.repKeeper().CreateAppealInitiative(ctx, "service.slash", payload, deadline)
		if err != nil {
			return err
		}
		report.JuryCaseId = caseID
	}

	if err := k.PendingReportsQueue.Remove(ctx, collections.Join(report.FiledAt, report.ReportId)); err != nil {
		return err
	}
	if err := k.EscalatedReportsQueue.Set(ctx, collections.Join(currentHeight, report.ReportId)); err != nil {
		return err
	}
	if err := k.Reports.Set(ctx, report.ReportId, *report); err != nil {
		return err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		types.NewReportEscalatedEvent(report.ReportId, report.JuryCaseId, proposedSlashBps),
	)
	return nil
}

// ---------------------------------------------------------------------------
// Helpers shared with contest / jury / EndBlocker paths
// ---------------------------------------------------------------------------

// refundReportDeposit returns the report's escrowed deposit to the
// reporter. No-op if the deposit is zero.
func (k msgServer) refundReportDeposit(ctx context.Context, report *types.Report) error {
	if report.Deposit.IsNil() || report.Deposit.IsZero() {
		return nil
	}
	reporterBytes, err := k.addrBytes(report.Reporter)
	if err != nil {
		return err
	}
	depositCoin := sdk.NewCoin(k.BondDenom(ctx), report.Deposit)
	return k.bankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		types.ModuleName,
		sdk.AccAddress(reporterBytes),
		sdk.NewCoins(depositCoin),
	)
}

// persistReportClosure writes the report row and drops it from the
// PendingReportsQueue. Used by T1_DISMISS and T1_SLASH (which keep
// status RESOLVED_T1 until the contest window passes; contest cleanup
// happens in MsgContestSlash or the EndBlocker escrow sweep).
func (k msgServer) persistReportClosure(ctx context.Context, report *types.Report, _ int64) error {
	if err := k.PendingReportsQueue.Remove(ctx, collections.Join(report.FiledAt, report.ReportId)); err != nil {
		return err
	}
	return k.Reports.Set(ctx, report.ReportId, *report)
}

// mustAddrBytes is a panic-on-invalid wrapper for use in code paths
// that have already validated the address upstream. Use sparingly —
// prefer explicit error handling at boundaries.
func mustAddrBytes(k msgServer, addr string) []byte {
	b, err := k.addrBytes(addr)
	if err != nil {
		// We control where this is called; an invalid address here
		// represents corrupt state, not a user error.
		panic(err)
	}
	return b
}
