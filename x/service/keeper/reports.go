package keeper

import (
	"context"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/service/types"
)

// reports.go holds the slashing & reporting state machine helpers shared
// across Msg{Report,Resolve,Contest,ResolveByJury}Operator handlers. See
// x-service-spec.md §3.4 for the full model.

// ---------------------------------------------------------------------------
// Reporter rate-limit (§3.4.6 sliding-window ring buffer)
// ---------------------------------------------------------------------------

// checkReporterRateLimit returns nil if the reporter is allowed to file
// a new report against (operator, serviceType) at currentHeight per the
// sliding-window rule (§3.4.6). Returns ErrReporterRateLimitExceeded if
// the cap is met.
//
// This is a pure read; the caller updates the ring buffer with
// recordReporterFiling after all other validation passes.
func (k Keeper) checkReporterRateLimit(
	ctx context.Context,
	params types.Params,
	reporterBytes, opBytes []byte,
	serviceType string,
	currentHeight int64,
) error {
	key := collections.Join3(reporterBytes, opBytes, serviceType)
	entry, err := k.ReporterRateLimit.Get(ctx, key)
	if err != nil {
		// Missing entry = no prior filings = always allowed.
		return nil
	}

	windowStart := currentHeight - params.ReporterRateLimitWindowBlocks
	var inWindow uint32
	for _, h := range entry.RecentFilingHeights {
		if h > windowStart && h <= currentHeight {
			inWindow++
		}
	}
	if inWindow >= params.MaxReportsPerReporterPerOperatorPerWindow {
		return types.ErrReporterRateLimitExceeded.Wrapf(
			"%d in last %d blocks (cap %d)",
			inWindow, params.ReporterRateLimitWindowBlocks, params.MaxReportsPerReporterPerOperatorPerWindow,
		)
	}
	return nil
}

// recordReporterFiling appends currentHeight to the reporter's ring buffer
// for (operator, serviceType), trimming oldest entries to keep length at
// max_reports + 1.
func (k Keeper) recordReporterFiling(
	ctx context.Context,
	params types.Params,
	reporterBytes, opBytes []byte,
	serviceType string,
	currentHeight int64,
	reporterAddrStr, opAddrStr string,
) error {
	key := collections.Join3(reporterBytes, opBytes, serviceType)
	entry, err := k.ReporterRateLimit.Get(ctx, key)
	if err != nil {
		entry = types.ReporterRateLimit{
			Reporter:        reporterAddrStr,
			OperatorAddress: opAddrStr,
			ServiceType:     serviceType,
		}
	}

	entry.RecentFilingHeights = append(entry.RecentFilingHeights, currentHeight)

	// Trim oldest entries (front of slice) to cap at max_reports + 1.
	cap := int(params.MaxReportsPerReporterPerOperatorPerWindow) + 1
	if len(entry.RecentFilingHeights) > cap {
		entry.RecentFilingHeights = entry.RecentFilingHeights[len(entry.RecentFilingHeights)-cap:]
	}

	return k.ReporterRateLimit.Set(ctx, key, entry)
}

// ---------------------------------------------------------------------------
// Refile-cooldown (§3.4.5)
// ---------------------------------------------------------------------------

// isRefileCooldownActive reports whether reporter is barred from filing
// against (operator, serviceType): one of their own member reports against
// it was AUTO_DISMISSED within report_refile_cooldown_blocks (§3.4.5).
//
// The cooldown is per reporter. It used to be keyed on the operator's
// controller and checked against EVERY reporter, so a single lapsed report
// — or a liveness report the controller let lapse — locked all members out
// of reporting the operator, exactly while it was misbehaving. Now the
// reporter whose allegation lapsed cannot re-file it, which is what stops
// the "file, let it lapse, re-file" loop that holds an operator's bond
// exit hostage; everyone else can still report.
func (k Keeper) isRefileCooldownActive(
	ctx context.Context,
	params types.Params,
	reporterBytes, opBytes []byte,
	serviceType string,
	currentHeight int64,
) (bool, error) {
	// RefileCooldowns keyed by (reporter, op, service_type, dismissed_at).
	// Quad ranges only prefix on K1; filter K2/K3 in the loop. Small
	// per-reporter scan.
	rng := collections.NewPrefixedQuadRange[[]byte, []byte, string, int64](reporterBytes)
	iter, err := k.RefileCooldowns.Iterate(ctx, rng)
	if err != nil {
		return false, err
	}

	var expired []collections.Quad[[]byte, []byte, string, int64]
	active := false
	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			iter.Close()
			return false, err
		}
		if string(key.K2()) != string(opBytes) || key.K3() != serviceType {
			continue
		}
		if currentHeight < key.K4()+params.ReportRefileCooldownBlocks {
			active = true
			break
		}
		expired = append(expired, key)
	}
	// Expired — lazy prune, after the iterator is done with the store.
	iter.Close()
	for _, key := range expired {
		if err := k.RefileCooldowns.Remove(ctx, key); err != nil {
			return false, err
		}
	}
	return active, nil
}

// recordRefileCooldown bars reporter from re-filing against (op,
// serviceType) from dismissedAt. Called from the EndBlocker pending sweep
// when a member report is AUTO_DISMISSED (§3.6 queue 2); system reports
// record none.
func (k Keeper) recordRefileCooldown(
	ctx context.Context,
	controllerStr, reporterStr, opStr, serviceType string,
	dismissedAt int64,
) error {
	reporterBytes, err := k.addrBytes(reporterStr)
	if err != nil {
		return err
	}
	opBytes, err := k.addrBytes(opStr)
	if err != nil {
		return err
	}
	return k.RefileCooldowns.Set(
		ctx,
		collections.Join4(reporterBytes, opBytes, serviceType, dismissedAt),
		types.RefileCooldown{
			Controller:      controllerStr,
			OperatorAddress: opStr,
			ServiceType:     serviceType,
			DismissedAt:     dismissedAt,
			Reporter:        reporterStr,
		},
	)
}

// isSystemReport reports whether report was filed through OpenSystemReport
// (a caller module or x/service's own liveness sweep) rather than by a
// member. Member reports always escrow a positive deposit
// (report_deposit_amount must be positive); system reports never do.
func isSystemReport(report types.Report) bool {
	return report.Deposit.IsNil() || report.Deposit.IsZero()
}

// ---------------------------------------------------------------------------
// Slash math (§3.4.2) + aggregate cap & cooldown (§3.4.3-§3.4.4)
// ---------------------------------------------------------------------------

// computeSlashAmount returns floor(currentBond * bps / 10000). Bps must
// be in [0, 10000]; caller is responsible for validating.
func computeSlashAmount(currentBond sdkmath.Int, bps uint32) sdkmath.Int {
	if currentBond.IsNil() || currentBond.IsZero() || bps == 0 {
		return sdkmath.ZeroInt()
	}
	// floor(bond * bps / 10000)
	return currentBond.Mul(sdkmath.NewInt(int64(bps))).Quo(sdkmath.NewInt(10000))
}

// checkAndAdvanceTier1Window advances the operator's tier-1 rolling
// window if it has expired, then returns the projected tier1_slashed
// after the candidate slash. Returns an error if the candidate slash
// would exceed the aggregate cap (§3.4.3).
//
// Mutates `op` in place: tier1_window_start, tier1_window_start_bond,
// and tier1_slashed_in_window may be reset if the window rolled over.
// Caller MUST persist the operator afterward.
func (k Keeper) checkAndAdvanceTier1Window(
	op *types.Operator,
	cfg types.ServiceTypeConfig,
	candidateSlash sdkmath.Int,
	currentHeight int64,
) error {
	windowBlocks := cfg.Tier1WindowBlocks
	aggregateCapBps := cfg.Tier1AggregateCapBps
	if windowBlocks == 0 || aggregateCapBps == 0 {
		// Per-type override missing → fall back to module defaults
		// (caller should have resolved these; defensive guard).
		return types.ErrInvalidServiceTypeConfig.Wrap("tier1 window/cap not set")
	}

	if currentHeight >= op.Tier1WindowStart+windowBlocks {
		// Window has slid; reset BEFORE applying the new slash.
		op.Tier1WindowStart = currentHeight
		op.Tier1WindowStartBond = op.BondAmount
		op.Tier1SlashedInWindow = sdkmath.ZeroInt()
	}

	if op.Tier1SlashedInWindow.IsNil() {
		op.Tier1SlashedInWindow = sdkmath.ZeroInt()
	}

	// Aggregate cap: tier1_slashed + candidate <= window_start_bond * cap / 10000.
	allowed := op.Tier1WindowStartBond.
		Mul(sdkmath.NewInt(int64(aggregateCapBps))).
		Quo(sdkmath.NewInt(10000))
	projected := op.Tier1SlashedInWindow.Add(candidateSlash)
	if projected.GT(allowed) {
		return types.ErrTier1AggregateCapExceeded.Wrapf(
			"projected %s > allowed %s (window start bond %s × %d bps)",
			projected, allowed, op.Tier1WindowStartBond, aggregateCapBps,
		)
	}

	return nil
}

// checkTier1Cooldown returns nil if the controller has not slashed this
// operator within cfg.tier1_cooldown_blocks. Pure read.
func (k Keeper) checkTier1Cooldown(
	ctx context.Context,
	cfg types.ServiceTypeConfig,
	controllerBytes, opBytes []byte,
	serviceType string,
	currentHeight int64,
) error {
	last, err := k.Tier1LastSlash.Get(ctx, collections.Join3(controllerBytes, opBytes, serviceType))
	if err != nil {
		return nil // no prior slash = never on cooldown
	}
	if currentHeight < last.LastSlashHeight+cfg.Tier1CooldownBlocks {
		return types.ErrTier1CooldownActive.Wrapf(
			"last_slash=%d, cooldown_blocks=%d, current=%d",
			last.LastSlashHeight, cfg.Tier1CooldownBlocks, currentHeight,
		)
	}
	return nil
}

// recordTier1Slash sets Tier1LastSlash[(controller, op, serviceType)] =
// currentHeight after a successful tier-1 resolution.
func (k Keeper) recordTier1Slash(
	ctx context.Context,
	controllerStr, opStr, serviceType string,
	currentHeight int64,
) error {
	controllerBytes, err := k.addrBytes(controllerStr)
	if err != nil {
		return err
	}
	opBytes, err := k.addrBytes(opStr)
	if err != nil {
		return err
	}
	return k.Tier1LastSlash.Set(
		ctx,
		collections.Join3(controllerBytes, opBytes, serviceType),
		types.Tier1LastSlash{
			Controller:      controllerStr,
			OperatorAddress: opStr,
			ServiceType:     serviceType,
			LastSlashHeight: currentHeight,
		},
	)
}

// ---------------------------------------------------------------------------
// Bond mutation + status flip on slash
// ---------------------------------------------------------------------------

// applySlashToBond debits slashAmount from op.BondAmount and flips
// status to UNDERFUNDED if the new bond falls below min_bond. Caller is
// responsible for moving SPARK out of the bond pool (to Tier1Escrow for
// T1, or community pool for T2/dissolution) and for persisting the
// operator.
//
// Does NOT settle bond-blocks — caller MUST call settleBondBlocks before
// invoking this (§6.6 "settle before any mutation to op.bond").
//
// Does NOT archive on bond==0 — caller decides whether to archive based
// on slash source (T1 zero-bond does NOT auto-archive since the escrow
// path could restore; T2 ACCEPT or dissolve=true does archive).
func (k Keeper) applySlashToBond(
	op *types.Operator,
	cfg types.ServiceTypeConfig,
	slashAmount sdkmath.Int,
	currentHeight int64,
) {
	if slashAmount.IsZero() {
		return
	}

	newBondAmount := op.BondAmount.Sub(slashAmount)
	if newBondAmount.IsNegative() {
		// Shouldn't happen given computeSlashAmount is floor(bond * bps / 10000),
		// but defensive.
		newBondAmount = sdkmath.ZeroInt()
	}
	op.BondAmount = newBondAmount
	clampPendingWithdrawal(op)

	// UNDERFUNDED transition: bond < min_bond AND status was ACTIVE.
	if op.Status == types.OperatorStatus_OPERATOR_STATUS_ACTIVE &&
		newBondAmount.LT(cfg.MinBondAmount) {
		op.Status = types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED
		op.UnderfundedSince = currentHeight
	}
}

// ---------------------------------------------------------------------------
// Open-reports check (for MsgClaimUnbondedBond)
// ---------------------------------------------------------------------------

// HasOpenReports reports whether any PENDING or ESCALATED report exists
// against (op_address, serviceType). Used by MsgClaimUnbondedBond
// eligibility check (§3.5).
func (k Keeper) HasOpenReports(ctx context.Context, opBytes []byte, serviceType string) (bool, error) {
	// Use ReportsByOperator secondary index to scope the scan.
	rng := collections.NewPrefixedTripleRange[[]byte, string, uint64](opBytes)
	iter, err := k.ReportsByOperator.Iterate(ctx, rng)
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
		reportID := key.K3()
		report, err := k.Reports.Get(ctx, reportID)
		if err != nil {
			continue
		}
		if report.Status == types.ReportStatus_REPORT_STATUS_PENDING ||
			report.Status == types.ReportStatus_REPORT_STATUS_ESCALATED {
			return true, nil
		}
	}
	return false, nil
}

// eagerReleaseExpiredEscrowsForOperator finalizes any Tier1Escrow rows for
// (op, serviceType) whose release_at has passed and that are linked to
// a terminally-resolved report, through releaseTier1Escrow (community pool
// plus the reputation deduction, exactly as the EndBlocker sweep does).
// Called by MsgClaimUnbondedBond and the partial-withdrawal claim so an
// operator isn't blocked waiting for the EndBlocker sweep (§3.5).
//
// Only releases entries whose parent report is in a terminal state
// (RESOLVED_T1 with contest window passed, RESOLVED_T2, or
// AUTO_TIMEOUT). Open-state reports (PENDING, ESCALATED) should have
// blocked the claim via HasOpenReports before we reach this code path,
// so any escrow we see here should be safe to release.
func (k Keeper) eagerReleaseExpiredEscrowsForOperator(
	ctx context.Context,
	opBytes []byte,
	serviceType string,
	currentHeight int64,
) error {
	rng := collections.NewPrefixedTripleRange[[]byte, string, uint64](opBytes)
	iter, err := k.Tier1EscrowByOperator.Iterate(ctx, rng)
	if err != nil {
		return err
	}
	defer iter.Close()

	type pending struct {
		escrowID uint64
		escrow   types.Tier1EscrowEntry
	}
	var toRelease []pending

	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return err
		}
		if key.K2() != serviceType {
			continue
		}
		escrowID := key.K3()
		escrow, err := k.Tier1Escrow.Get(ctx, escrowID)
		if err != nil {
			continue
		}
		if escrow.ReleaseAt > currentHeight {
			// Still within contest window — defensive guard; the claim
			// handler's HasActiveTier1Escrow check should have already
			// rejected.
			continue
		}
		// Defer collection until after iteration to avoid mutating the
		// store while iterating.
		toRelease = append(toRelease, pending{escrowID, escrow})
	}

	for _, p := range toRelease {
		if err := k.releaseTier1Escrow(ctx, p.escrowID, p.escrow, currentHeight); err != nil {
			return err
		}
	}
	return nil
}

// bankModuleAddress returns the x/service module account address.
// Used as the `sender` argument to distribution.FundCommunityPool
// (which expects sdk.AccAddress and withdraws from sender to the
// community pool).
func (k Keeper) bankModuleAddress() sdk.AccAddress {
	return authtypes.NewModuleAddress(types.ModuleName)
}

// grantReputationOnClaim computes the §6.6 effective_bond_blocks (max
// single-record bond-blocks across the address's currently-live ACTIVE
// operators) for the unbonding operator and writes the grant through
// to x/rep's `service-operator` tag.
//
// Called from MsgClaimUnbondedBond after the bond return and before
// the archive transition. The unbonding record being claimed is still
// in the live store at this point (will be moved to archive after);
// it is included in the max but the cap is the single largest record,
// so multiple registrations don't multiply the grant.
//
// No-op if repKeeper isn't wired (standalone dev) or
// reputation_grant_per_bond_block is zero.
func (k Keeper) grantReputationOnClaim(
	ctx context.Context,
	claimingOp types.Operator,
	opAddrBytes []byte,
) error {
	if k.repKeeper() == nil {
		return nil
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	if params.ReputationGrantPerBondBlock.IsNil() || params.ReputationGrantPerBondBlock.IsZero() {
		return nil
	}

	// Start with the claiming operator's own bond-block accrual.
	effective := claimingOp.TotalLifetimeBondBlocks
	if effective.IsNil() {
		effective = sdkmath.ZeroInt()
	}

	// Iterate other live records owned by this address; take the max.
	rng := collections.NewPrefixedPairRange[[]byte, string](opAddrBytes)
	iter, err := k.Operators.Iterate(ctx, rng)
	if err != nil {
		return err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return err
		}
		// Skip the operator being claimed (we already included it).
		if key.K2() == claimingOp.ServiceType {
			continue
		}
		op, err := iter.Value()
		if err != nil {
			return err
		}
		if !op.TotalLifetimeBondBlocks.IsNil() && op.TotalLifetimeBondBlocks.GT(effective) {
			effective = op.TotalLifetimeBondBlocks
		}
	}

	if effective.IsZero() {
		return nil
	}

	grant := params.ReputationGrantPerBondBlock.MulInt(effective)
	return k.repKeeper().AddReputation(
		ctx,
		sdk.AccAddress(opAddrBytes),
		types.ReputationTagServiceOperator,
		grant,
	)
}

// deductReputationOnSlash applies the §6.6 reputation deduction for a
// slash that has become final: a tier-1 slash whose escrow released to
// the community pool uncontested, or a tier-2 jury ACCEPT/REDUCE. The
// deduction is proportional to the slash: the share of the bond taken
// (slashAmount / bondBefore) applied to the reputation the record has
// accrued so far (total_lifetime_bond_blocks * reputation_grant_per_bond_block),
// so it is denominated exactly like the grant paid at unbond claim.
// `multiplier` is 1 for a tier-1 slash and
// types.JurySlashReputationMultiplier for a jury slash; the result never
// exceeds the accrued reputation.
//
// Best-effort like grantReputationOnClaim: the caller logs and proceeds
// when rep is unwired, the param is zero, or the operator is not a rep
// member (DeductReputation returns ErrMemberNotFound).
func (k Keeper) deductReputationOnSlash(
	ctx context.Context,
	op types.Operator,
	opAddrBytes []byte,
	slashAmount sdkmath.Int,
	bondBefore sdkmath.Int,
	multiplier int64,
) error {
	if k.repKeeper() == nil || slashAmount.IsNil() || !slashAmount.IsPositive() ||
		bondBefore.IsNil() || !bondBefore.IsPositive() {
		return nil
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	if params.ReputationGrantPerBondBlock.IsNil() || params.ReputationGrantPerBondBlock.IsZero() {
		return nil
	}
	accrued := op.TotalLifetimeBondBlocks
	if accrued.IsNil() || accrued.IsZero() {
		return nil
	}
	if slashAmount.GT(bondBefore) {
		slashAmount = bondBefore
	}
	accruedRep := params.ReputationGrantPerBondBlock.MulInt(accrued)
	deduction := accruedRep.MulInt(slashAmount).QuoInt(bondBefore)
	if multiplier > 1 {
		deduction = deduction.MulInt64(multiplier)
	}
	if deduction.GT(accruedRep) {
		deduction = accruedRep
	}
	if !deduction.IsPositive() {
		return nil
	}
	return k.repKeeper().DeductReputation(
		ctx,
		sdk.AccAddress(opAddrBytes),
		types.ReputationTagServiceOperator,
		deduction,
	)
}

// closeOpenReportsOnDissolve closes all PENDING and ESCALATED reports
// against (op, serviceType) with status CLOSED_OPERATOR_DISSOLVED,
// refunds reporter deposits, and cancels any open jury cases. Called
// from dissolveOperator (§3.4.9).
//
// Does NOT touch Tier1Escrow entries — those are released to community
// pool by the standard sweep (the operator is being slashed; any
// escrowed slash amounts genuinely belong to the pool).
func (k Keeper) closeOpenReportsOnDissolve(
	ctx context.Context,
	opBytes []byte,
	serviceType string,
) error {
	rng := collections.NewPrefixedTripleRange[[]byte, string, uint64](opBytes)
	iter, err := k.ReportsByOperator.Iterate(ctx, rng)
	if err != nil {
		return err
	}
	defer iter.Close()

	var toClose []uint64
	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return err
		}
		if key.K2() != serviceType {
			continue
		}
		toClose = append(toClose, key.K3())
	}

	for _, reportID := range toClose {
		report, err := k.Reports.Get(ctx, reportID)
		if err != nil {
			continue
		}
		if report.Status != types.ReportStatus_REPORT_STATUS_PENDING &&
			report.Status != types.ReportStatus_REPORT_STATUS_ESCALATED {
			continue
		}

		// Refund reporter deposit (reporters bore no fault — §3.4.9).
		depositCoin := sdk.NewCoin(k.BondDenom(ctx), report.Deposit)
		if !report.Deposit.IsNil() && !report.Deposit.IsZero() {
			reporterBytes, err := k.addrBytes(report.Reporter)
			if err == nil {
				if err := k.bankKeeper.SendCoinsFromModuleToAccount(
					ctx, types.ModuleName, sdk.AccAddress(reporterBytes), sdk.NewCoins(depositCoin),
				); err != nil {
					return err
				}
			}
		}

		// Any ESCALATED report's parallel JuryReview is left in place —
		// x/rep does not expose a cancel API. A still-PENDING JuryReview
		// pointing to a CLOSED_OPERATOR_DISSOLVED report is harmless;
		// the resolver-side handlers reject any late verdict that lands
		// against a non-ESCALATED report (§5.2).

		// Drop queue index entries.
		switch report.Status {
		case types.ReportStatus_REPORT_STATUS_PENDING:
			if err := k.PendingReportsQueue.Remove(ctx, collections.Join(report.FiledAt, report.ReportId)); err != nil {
				return err
			}
		case types.ReportStatus_REPORT_STATUS_ESCALATED:
			if err := k.EscalatedReportsQueue.Remove(ctx, collections.Join(report.EscalatedAt, report.ReportId)); err != nil {
				return err
			}
		}

		// Mark CLOSED_OPERATOR_DISSOLVED.
		report.Status = types.ReportStatus_REPORT_STATUS_CLOSED_OPERATOR_DISSOLVED
		if err := k.Reports.Set(ctx, reportID, report); err != nil {
			return err
		}
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
			types.NewReportClosedDissolvedEvent(report.ReportId, depositCoin),
		)
	}

	return nil
}

// HasActiveTier1Escrow reports whether any Tier1Escrow entry exists for
// (op_address, serviceType) with release_at > currentHeight (i.e. still
// genuinely within its contest window). Expired-but-unswept entries
// don't count — they should be processed inline by the claim handler
// (§3.5 / §3.6 queue 4).
func (k Keeper) HasActiveTier1Escrow(ctx context.Context, opBytes []byte, serviceType string, currentHeight int64) (bool, error) {
	rng := collections.NewPrefixedTripleRange[[]byte, string, uint64](opBytes)
	iter, err := k.Tier1EscrowByOperator.Iterate(ctx, rng)
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
		escrowID := key.K3()
		escrow, err := k.Tier1Escrow.Get(ctx, escrowID)
		if err != nil {
			continue
		}
		if escrow.ReleaseAt > currentHeight {
			return true, nil
		}
	}
	return false, nil
}

// juryVotingDeadline is the block height by which a service jury must vote:
// the first half of max_escalated_blocks. The second half is left for the
// resolver to relay the tallied verdict (MsgResolveReportByJury /
// MsgFinalizeControllerTransfer) before the escalated sweep auto-times the
// case out at escalated_at + max_escalated_blocks. With both deadlines equal
// a verdict tallied at the deadline could never be relayed.
func juryVotingDeadline(currentHeight int64, params types.Params) int64 {
	half := params.MaxEscalatedBlocks / 2
	if half < 1 {
		half = 1
	}
	return currentHeight + half
}

// slashCaseParties lists the addresses kept off a service.slash jury: the
// operator, the reporter, the operator's controller and every member of
// the controller group.
func (k Keeper) slashCaseParties(ctx context.Context, report types.Report) []string {
	parties := []string{report.OperatorAddress, report.Reporter}
	if opBytes, err := k.addrBytes(report.OperatorAddress); err == nil {
		if op, ok := k.GetOperator(ctx, opBytes, report.ServiceType); ok {
			parties = append(parties, k.groupParties(ctx, op.Controller)...)
		}
	}
	return parties
}

// groupParties returns a controller group's policy address followed by its
// members. A policy address is never a juror, so excluding only it would
// let the members who voted a decision sit on the jury reviewing it
// (SVC-102); x/rep then keeps every address of each member off the jury.
func (k Keeper) groupParties(ctx context.Context, policyAddr string) []string {
	out := []string{policyAddr}
	if policyAddr == "" || k.commonsKeeper() == nil {
		return out
	}
	members, err := k.commonsKeeper().GroupPolicyMembers(ctx, policyAddr)
	if err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("groupParties: GroupPolicyMembers failed", "policy", policyAddr, "err", err)
		return out
	}
	return append(out, members...)
}
