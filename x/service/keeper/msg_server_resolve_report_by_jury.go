package keeper

import (
	"context"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/service/types"
)

// ResolveReportByJury applies a tier-2 jury verdict to an ESCALATED
// report. See x-service-spec.md §5.2 + §6.2. Signer MUST be a member
// of the Commons Council Operations Committee — same pattern as
// `MsgResolveGovActionAppeal` and forum's appeal-resolve handlers.
// The resolver acts as a low-trust relayer: the on-chain integrity
// check is the verdict cross-check against x/rep's JuryReview.
//
// Verdict semantics (mapping to x/rep verdicts in §6.2):
//   - ACCEPT: requires JuryReview verdict == VERDICT_UPHOLD_CHALLENGE.
//     Apply full proposed_slash_bps. dissolve=true ⇒ unconditional
//     SLASHED.
//   - REDUCE: also requires JuryReview verdict == VERDICT_UPHOLD_CHALLENGE.
//     Apply a smaller slash; remainder of any contested escrow returns
//     to bond.
//   - REJECT: requires JuryReview verdict ∈ {VERDICT_REJECT_CHALLENGE,
//     VERDICT_INCONCLUSIVE}. No slash; contested escrow (if any)
//     returns to bond.
//
// On all paths the report transitions to RESOLVED_T2 and is removed
// from EscalatedReportsQueue. Reporter deposit is refunded on
// ACCEPT/REDUCE, forfeited to community pool on REJECT.
//
// Note: msg.JuryAuthority is the resolver's bech32 address. The
// proto field name is retained for backward compatibility with the
// scaffolded code; conceptually it means "the council member relaying
// the jury verdict" rather than "the x/rep module account."
func (k msgServer) ResolveReportByJury(ctx context.Context, msg *types.MsgResolveReportByJury) (*types.MsgResolveReportByJuryResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	// Authority gate: Commons Operations Committee member (§5.2 + §6.2).
	if err := k.assertCouncilResolver(ctx, msg.JuryAuthority); err != nil {
		return nil, err
	}

	report, err := k.Reports.Get(ctx, msg.ReportId)
	if err != nil {
		return nil, types.ErrReportNotFound.Wrap(err.Error())
	}
	if report.Status != types.ReportStatus_REPORT_STATUS_ESCALATED {
		return nil, types.ErrInvalidVerdict.Wrap("report is not ESCALATED")
	}

	// Verdict cross-check against the underlying JuryReview (§6.2).
	// Skipped in standalone-mode when repKeeper is not wired.
	if err := k.crossCheckSlashVerdict(ctx, report.JuryCaseId, msg.Verdict); err != nil {
		return nil, err
	}

	opBytes, err := k.addrBytes(report.OperatorAddress)
	if err != nil {
		return nil, err
	}
	op, exists := k.GetOperator(ctx, opBytes, report.ServiceType)
	if !exists {
		// Operator archived since escalation — shouldn't happen because
		// dissolution closes ESCALATED reports as CLOSED_OPERATOR_DISSOLVED,
		// but defensive guard.
		return nil, types.ErrOperatorNotFound
	}

	cfg, err := k.resolveServiceTypeConfig(ctx, op.ServiceType)
	if err != nil {
		return nil, err
	}

	// Verdict ↔ slash_bps relationship (§5.2 table).
	switch msg.Verdict {
	case types.JuryVerdict_JURY_VERDICT_ACCEPT:
		if msg.SlashBps != report.ProposedSlashBps {
			return nil, types.ErrInvalidVerdict.Wrapf(
				"ACCEPT requires slash_bps==proposed (%d != %d)",
				msg.SlashBps, report.ProposedSlashBps,
			)
		}
	case types.JuryVerdict_JURY_VERDICT_REDUCE:
		if msg.SlashBps == 0 || msg.SlashBps >= report.ProposedSlashBps {
			return nil, types.ErrInvalidVerdict.Wrap("REDUCE requires 0 < slash_bps < proposed")
		}
		if msg.Dissolve {
			return nil, types.ErrInvalidVerdict.Wrap("REDUCE cannot dissolve")
		}
	case types.JuryVerdict_JURY_VERDICT_REJECT:
		if msg.SlashBps != 0 {
			return nil, types.ErrInvalidVerdict.Wrap("REJECT requires slash_bps == 0")
		}
		if msg.Dissolve {
			return nil, types.ErrInvalidVerdict.Wrap("REJECT cannot dissolve")
		}
	default:
		return nil, types.ErrInvalidVerdict.Wrap(msg.Verdict.String())
	}

	// Find any escrow that may still be holding contested T1 funds for
	// this report. If found, we re-route it based on verdict; if not,
	// any slash is taken directly from bond.
	escrow, escrowID, hasEscrow, err := k.findTier1EscrowForReport(ctx, opBytes, report.ServiceType, report.ReportId)
	if err != nil {
		return nil, err
	}

	// Compute the slash amount the jury verdict imposes. Bond-blocks
	// settled BEFORE the bond change (§6.6).
	k.settleBondBlocks(&op, currentHeight)

	// Capture pre-mutation status so we can detect ACTIVE → UNDERFUNDED
	// or UNDERFUNDED → ACTIVE transitions for hook firing after the
	// state writes complete (Phase 0 federation→service migration).
	preStatus := op.Status
	underfundedTransition := false

	bondDenom := k.BondDenom(ctx)

	var slashAmount sdkmath.Int
	if msg.Verdict == types.JuryVerdict_JURY_VERDICT_REJECT {
		slashAmount = sdkmath.ZeroInt()
	} else {
		// ACCEPT or REDUCE — compute on whichever pool the slash is
		// drawn from. If we have a contested escrow, the slash is
		// bounded by the escrow amount; the difference returns to bond.
		// If no escrow, the slash debits the current bond directly.
		basis := op.BondAmount
		if hasEscrow {
			basis = escrow.Amount
		}
		slashAmount = computeSlashAmount(basis, msg.SlashBps)
	}

	switch {
	case hasEscrow && slashAmount.IsZero():
		// REJECT (or zero-bps REDUCE — caught above): return full escrow
		// to operator's bond.
		op.BondAmount = op.BondAmount.Add(escrow.Amount)
	case hasEscrow:
		// Slash is bounded by the escrow. Pay slashAmount from module
		// account → community pool; return the rest (escrow.Amount - slash)
		// to operator's bond.
		if slashAmount.GT(escrow.Amount) {
			// Defensive: REDUCE constraint should have prevented this.
			slashAmount = escrow.Amount
		}
		if !slashAmount.IsZero() && k.distributionKeeper() != nil {
			payout := sdk.NewCoin(bondDenom, slashAmount)
			if err := k.distributionKeeper().FundCommunityPool(ctx, sdk.NewCoins(payout), k.bankModuleAddress()); err != nil {
				return nil, err
			}
		}
		refund := escrow.Amount.Sub(slashAmount)
		if refund.IsPositive() {
			op.BondAmount = op.BondAmount.Add(refund)
		}
	default:
		// Direct escalation (no prior T1). Debit op.BondAmount and pay
		// slash to community pool.
		if !slashAmount.IsZero() {
			if slashAmount.GT(op.BondAmount) {
				slashAmount = op.BondAmount
			}
			op.BondAmount = op.BondAmount.Sub(slashAmount)
			if k.distributionKeeper() != nil {
				if err := k.distributionKeeper().FundCommunityPool(
					ctx,
					sdk.NewCoins(sdk.NewCoin(bondDenom, slashAmount)),
					k.bankModuleAddress(),
				); err != nil {
					return nil, err
				}
			}
			// Status may flip ACTIVE → UNDERFUNDED — borrow applySlashToBond's
			// rule rather than re-implementing.
			if op.Status == types.OperatorStatus_OPERATOR_STATUS_ACTIVE &&
				op.BondAmount.LT(cfg.MinBondAmount) {
				op.Status = types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED
				op.UnderfundedSince = currentHeight
				underfundedTransition = true
			}
		}
	}

	// Clean up the escrow row + indexes regardless of verdict (the
	// contested funds have been routed by this point).
	if hasEscrow {
		if err := k.Tier1Escrow.Remove(ctx, escrowID); err != nil {
			return nil, err
		}
		_ = k.Tier1EscrowByOperator.Remove(ctx, collections.Join3(opBytes, op.ServiceType, escrowID))
		_ = k.Tier1EscrowReleaseQueue.Remove(ctx, collections.Join(escrow.ReleaseAt, escrowID))
	}

	// Dissolve check (§5.2): ACCEPT with dissolve=true OR bond → 0 ⇒
	// SLASHED + archive + dissolution hook + close any open reports.
	dissolveNow := msg.Dissolve || (msg.Verdict != types.JuryVerdict_JURY_VERDICT_REJECT && op.BondAmount.IsZero())

	// Update report.
	report.Status = types.ReportStatus_REPORT_STATUS_RESOLVED_T2
	report.SlashAmount = slashAmount
	if err := k.Reports.Set(ctx, report.ReportId, report); err != nil {
		return nil, err
	}
	_ = k.EscalatedReportsQueue.Remove(ctx, collections.Join(report.EscalatedAt, report.ReportId))

	// Reporter deposit: refund on ACCEPT/REDUCE, forfeit on REJECT.
	if msg.Verdict == types.JuryVerdict_JURY_VERDICT_REJECT {
		if err := k.forfeitDepositToCommunityPool(ctx, report.Deposit); err != nil {
			return nil, err
		}
	} else {
		if err := k.refundDepositToReporter(ctx, report); err != nil {
			return nil, err
		}
	}

	if !dissolveNow {
		if err := k.PutOperator(ctx, op); err != nil {
			return nil, err
		}
	} else {
		if err := k.dissolveOperator(ctx, &op, currentHeight, report.ReportId); err != nil {
			return nil, err
		}
	}

	verdictStr := msg.Verdict.String()
	slashCoin := sdk.NewCoin(bondDenom, slashAmount)
	events := sdk.Events{
		types.NewReportResolvedT2Event(report.ReportId, verdictStr, slashCoin, msg.Dissolve),
	}
	if !slashAmount.IsZero() {
		events = append(events, types.NewOperatorSlashedEvent(
			op.Address, op.ServiceType,
			slashCoin,
			sdk.NewCoin(bondDenom, op.BondAmount), types.TierTier2, report.ReportId,
		))
	}
	sdkCtx.EventManager().EmitEvents(events)
	emitReportResolved(report.ServiceType, types.TierTier2, verdictStr)
	if !slashAmount.IsZero() {
		emitSlashAmount(op.ServiceType, types.TierTier2, float32(slashAmount.Int64()))
	}

	// AfterOperatorUnderfunded fires after state writes complete so
	// consumer hooks see the post-transition state. Suppressed when the
	// operator dissolved this block (AfterOperatorDissolved already
	// covers that). Phase 0 federation→service migration.
	if underfundedTransition && !dissolveNow && k.hooks() != nil {
		_ = preStatus // captured at top for symmetry; not currently used post-dissolve gate
		k.hooks().AfterOperatorUnderfunded(ctx, sdk.AccAddress(opBytes), op.ServiceType)
	}

	return &types.MsgResolveReportByJuryResponse{}, nil
}

// assertCouncilResolver gates jury-resolver msgs to members of the
// Commons Council Operations Committee. Same pattern as
// `MsgResolveGovActionAppeal` (x/rep) and forum's appeal-resolve
// handlers. In standalone-mode (commonsKeeper not wired) the gate is
// skipped — production wiring always supplies the keeper.
func (k Keeper) assertCouncilResolver(ctx context.Context, signer string) error {
	if _, err := k.addrBytes(signer); err != nil {
		return types.ErrUnauthorizedCouncilResolver.Wrap("invalid resolver address")
	}
	if k.commonsKeeper() == nil {
		// Standalone test mode — no gate enforceable. Production never
		// hits this branch because SetCrossModuleKeepers always supplies
		// commonsKeeper.
		return nil
	}
	if !k.commonsKeeper().IsCouncilAuthorized(ctx, signer, "commons", "operations") {
		return types.ErrUnauthorizedCouncilResolver.Wrap(signer)
	}
	return nil
}

// crossCheckSlashVerdict verifies that the resolver's submitted
// service.slash verdict is consistent with the underlying x/rep
// JuryReview's tallied verdict (§6.2 mapping table). Returns
// ErrJuryVerdictMismatch on disagreement; skipped in standalone mode.
//
//   - ACCEPT or REDUCE requires JuryReview verdict == UPHOLD_CHALLENGE
//   - REJECT requires JuryReview verdict ∈ {REJECT_CHALLENGE, INCONCLUSIVE}
//   - PENDING (jury hasn't tallied yet) always rejects — the resolver
//     must wait until x/rep's TallyJuryVotes runs.
func (k Keeper) crossCheckSlashVerdict(ctx context.Context, juryCaseID uint64, submittedVerdict types.JuryVerdict) error {
	if k.repKeeper() == nil || juryCaseID == 0 {
		return nil
	}
	verdictName, err := k.repKeeper().GetJuryVerdictName(ctx, juryCaseID)
	if err != nil {
		return types.ErrJuryVerdictMismatch.Wrapf("could not fetch JuryReview %d: %v", juryCaseID, err)
	}
	switch submittedVerdict {
	case types.JuryVerdict_JURY_VERDICT_ACCEPT, types.JuryVerdict_JURY_VERDICT_REDUCE:
		if verdictName != types.JuryVerdictUpholdChallenge {
			return types.ErrJuryVerdictMismatch.Wrapf(
				"resolver submitted %s but jury voted %s", submittedVerdict, verdictName,
			)
		}
	case types.JuryVerdict_JURY_VERDICT_REJECT:
		if verdictName != types.JuryVerdictRejectChallenge && verdictName != types.JuryVerdictInconclusive {
			return types.ErrJuryVerdictMismatch.Wrapf(
				"resolver submitted REJECT but jury voted %s", verdictName,
			)
		}
	}
	return nil
}

// refundDepositToReporter pays the report's deposit back to its
// reporter from the module account. No-op on zero deposit.
func (k Keeper) refundDepositToReporter(ctx context.Context, report types.Report) error {
	if report.Deposit.IsNil() || report.Deposit.IsZero() {
		return nil
	}
	reporterBytes, err := k.addrBytes(report.Reporter)
	if err != nil {
		return err
	}
	depositCoin := sdk.NewCoin(k.BondDenom(ctx), report.Deposit)
	return k.bankKeeper.SendCoinsFromModuleToAccount(
		ctx, types.ModuleName, sdk.AccAddress(reporterBytes), sdk.NewCoins(depositCoin),
	)
}

// forfeitDepositToCommunityPool routes the deposit amount to the
// community pool (REJECT verdict, §3.4.6). No-op on zero deposit or
// when distributionKeeper is unwired (standalone dev mode).
func (k Keeper) forfeitDepositToCommunityPool(ctx context.Context, depositAmount sdkmath.Int) error {
	if depositAmount.IsNil() || depositAmount.IsZero() {
		return nil
	}
	if k.distributionKeeper() == nil {
		return nil
	}
	depositCoin := sdk.NewCoin(k.BondDenom(ctx), depositAmount)
	return k.distributionKeeper().FundCommunityPool(ctx, sdk.NewCoins(depositCoin), k.bankModuleAddress())
}

// dissolveOperator transitions an operator to SLASHED and archives the
// record. Used by:
//   - MsgResolveReportByJury ACCEPT + dissolve=true (or bond → 0);
//   - the public TerminateOperator keeper method (§6.7).
//
// Side effects per §3.4.9:
//   - any remaining bond is transferred to the community pool;
//   - all open reports against the operator are closed as
//     CLOSED_OPERATOR_DISSOLVED (deposits refunded);
//   - record archived as SLASHED;
//   - ServiceHooks.AfterOperatorDissolved fires (so x/commons cancels
//     RecurringSpend schedules — §6.1);
//   - emits service.operator_dissolved + service.operator_archived events.
func (k Keeper) dissolveOperator(ctx context.Context, op *types.Operator, currentHeight int64, causingReportID uint64) error {
	// Capture confiscated amount BEFORE zeroing for the event.
	bondDenom := k.BondDenom(ctx)
	confiscated := sdk.NewCoin(bondDenom, op.BondAmount)

	// Transfer remaining bond to community pool.
	if !op.BondAmount.IsZero() && k.distributionKeeper() != nil {
		if err := k.distributionKeeper().FundCommunityPool(ctx, sdk.NewCoins(confiscated), k.bankModuleAddress()); err != nil {
			return err
		}
	}

	op.BondAmount = sdkmath.ZeroInt()
	op.Status = types.OperatorStatus_OPERATOR_STATUS_SLASHED
	op.RetiredAt = currentHeight

	// Close any open reports against this operator with deposit refunds
	// (§3.4.9).
	opBytes, err := k.addrBytes(op.Address)
	if err != nil {
		return err
	}
	if err := k.closeOpenReportsOnDissolve(ctx, opBytes, op.ServiceType); err != nil {
		return err
	}

	if err := k.ArchiveOperator(ctx, *op); err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvents(sdk.Events{
		types.NewOperatorDissolvedEvent(op.Address, op.ServiceType, confiscated, causingReportID),
		types.NewOperatorArchivedEvent(op.Address, op.ServiceType, "SLASHED", currentHeight),
	})
	emitOperatorDissolved(op.ServiceType, types.TierTier2)

	if k.hooks() != nil {
		k.hooks().AfterOperatorDissolved(ctx, sdk.AccAddress(opBytes), op.ServiceType)
	}

	return nil
}
