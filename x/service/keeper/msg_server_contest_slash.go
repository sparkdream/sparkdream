package keeper

import (
	"bytes"
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/service/types"
)

// ContestSlash escalates a RESOLVED_T1 report to the jury within the
// contest window (§5.2). On success:
//
//   - releases the corresponding Tier1Escrow entry back to the
//     operator's bond,
//   - decrements tier1_slashed_in_window (so the aggregate cap reflects
//     the contested slash being undone),
//   - reverts the operator's status to its pre-slash value (UNDERFUNDED →
//     ACTIVE if the contested slash was what dropped them below
//     min_bond; otherwise no status change),
//   - transitions the report PENDING-T1 → ESCALATED with proposed_slash_bps
//     carried over,
//   - opens an x/rep jury case via RepKeeper.CreateAppealInitiative (same
//     generic-jury entry-point used by MsgResolveReport's
//     ESCALATE_TO_JURY path — §6.2).
//
// The operator pays no extra deposit to contest — their bond is already
// staked (§5.2 wording).
func (k msgServer) ContestSlash(ctx context.Context, msg *types.MsgContestSlash) (*types.MsgContestSlashResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	opBytes, err := k.addrBytes(msg.Operator)
	if err != nil {
		return nil, types.ErrInvalidSigner.Wrap("invalid operator address")
	}

	report, err := k.Reports.Get(ctx, msg.ReportId)
	if err != nil {
		return nil, types.ErrReportNotFound.Wrap(err.Error())
	}

	// Signer must be the operator named on the report.
	reportOpBytes, err := k.addrBytes(report.OperatorAddress)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(opBytes, reportOpBytes) {
		return nil, types.ErrUnauthorizedController.Wrap("signer is not the operator on this report")
	}
	if report.ServiceType != msg.ServiceType {
		return nil, types.ErrInvalidVerdict.Wrap("service_type mismatch with report")
	}

	// Only RESOLVED_T1 reports are contestable.
	if report.Status != types.ReportStatus_REPORT_STATUS_RESOLVED_T1 {
		return nil, types.ErrInvalidVerdict.Wrap("report is not in RESOLVED_T1 state")
	}

	// Contest must be within the contest window. The window is anchored
	// off the escrow's release_at, which equals the resolution height +
	// report_contest_window_blocks. Find the matching escrow row and
	// reject if expired.
	op, exists := k.GetOperator(ctx, opBytes, msg.ServiceType)
	if !exists {
		return nil, types.ErrOperatorNotFound.Wrapf("%s / %s", msg.Operator, msg.ServiceType)
	}

	// Locate the Tier1Escrow entry that backs this report. There's at
	// most one per report (a T1_SLASH writes exactly one).
	escrow, escrowID, found, err := k.findTier1EscrowForReport(ctx, opBytes, msg.ServiceType, report.ReportId)
	if err != nil {
		return nil, err
	}
	if !found {
		// Escrow already swept to community pool — contest window has
		// passed.
		return nil, types.ErrContestWindowExpired.Wrap("escrow already released")
	}
	if currentHeight >= escrow.ReleaseAt {
		return nil, types.ErrContestWindowExpired.Wrapf(
			"release_at=%d, current=%d", escrow.ReleaseAt, currentHeight,
		)
	}

	cfg, err := k.resolveServiceTypeConfig(ctx, op.ServiceType)
	if err != nil {
		return nil, err
	}

	// Release escrowed SPARK back to the operator's bond. Funds stay in
	// the module account (just move accounting from the escrow pool to
	// the bond pool).
	k.settleBondBlocks(&op, currentHeight)
	op.Bond = op.Bond.Add(escrow.Amount)
	if !op.Tier1SlashedInWindow.IsNil() && !escrow.Amount.Amount.IsZero() {
		op.Tier1SlashedInWindow = op.Tier1SlashedInWindow.Sub(escrow.Amount.Amount)
		if op.Tier1SlashedInWindow.IsNegative() {
			op.Tier1SlashedInWindow = op.Tier1SlashedInWindow.MulRaw(0) // zero out
		}
	}

	// If the contested slash had pushed the operator UNDERFUNDED and
	// the restored bond clears min_bond, revert to ACTIVE.
	refundedTransition := false
	if op.Status == types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED &&
		op.Bond.Amount.GTE(cfg.MinBond.Amount) {
		oldUnderfundedSince := op.UnderfundedSince
		op.Status = types.OperatorStatus_OPERATOR_STATUS_ACTIVE
		op.UnderfundedSince = 0
		if oldUnderfundedSince > 0 {
			if err := k.removeUnderfundedQueueEntry(ctx, oldUnderfundedSince, opBytes, op.ServiceType); err != nil {
				return nil, err
			}
		}
		refundedTransition = true
	}

	if err := k.PutOperator(ctx, op); err != nil {
		return nil, err
	}

	// Delete the escrow row and its indexes (the funds it tracked have
	// been moved into the bond pool conceptually; physically they were
	// always in the module account).
	if err := k.Tier1Escrow.Remove(ctx, escrowID); err != nil {
		return nil, err
	}
	_ = k.Tier1EscrowByOperator.Remove(ctx, collections.Join3(opBytes, msg.ServiceType, escrowID))
	_ = k.Tier1EscrowReleaseQueue.Remove(ctx, collections.Join(escrow.ReleaseAt, escrowID))

	// Transition report RESOLVED_T1 → ESCALATED. proposed_slash_bps
	// carries the original tier-1 slash_bps as the jury's upper bound.
	report.Status = types.ReportStatus_REPORT_STATUS_ESCALATED
	report.EscalatedAt = currentHeight

	if k.repKeeper() != nil {
		params, err := k.Params.Get(ctx)
		if err != nil {
			return nil, err
		}
		payload := []byte(fmt.Sprintf(
			`{"report_id":%d,"operator":%q,"service_type":%q,"reporter":%q,"reason":%q,"proposed_slash_bps":%d,"contested":true}`,
			report.ReportId, report.OperatorAddress, report.ServiceType,
			report.Reporter, report.Reason, report.ProposedSlashBps,
		))
		deadline := currentHeight + params.MaxEscalatedBlocks
		caseID, err := k.repKeeper().CreateAppealInitiative(ctx, "service.slash", payload, deadline)
		if err != nil {
			return nil, err
		}
		report.JuryCaseId = caseID
	}
	if err := k.Reports.Set(ctx, report.ReportId, report); err != nil {
		return nil, err
	}
	if err := k.EscalatedReportsQueue.Set(ctx, collections.Join(currentHeight, report.ReportId)); err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvents(sdk.Events{
		types.NewReportContestedEvent(report.ReportId, escrow.Amount),
		types.NewTier1EscrowReleasedEvent(escrowID, report.ReportId, op.Address, op.ServiceType, escrow.Amount, types.EscrowDestBondRestored),
		types.NewReportEscalatedEvent(report.ReportId, report.JuryCaseId, report.ProposedSlashBps),
	})

	if refundedTransition && k.hooks() != nil {
		k.hooks().AfterOperatorReFunded(ctx, sdk.AccAddress(opBytes), op.ServiceType)
	}

	return &types.MsgContestSlashResponse{}, nil
}

// findTier1EscrowForReport scans the per-operator escrow index and
// returns the entry tagged with the given report_id (T1_SLASH writes
// exactly one per report). Returns found=false if no matching entry.
func (k msgServer) findTier1EscrowForReport(
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
