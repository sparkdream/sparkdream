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

// isRefileCooldownActive reports whether the (controller, op, serviceType)
// tuple is currently within a refile-cooldown window (set when a prior
// PENDING report from this controller against this operator was
// AUTO_DISMISSED — §3.4.5). Returns true if any active entry exists.
//
// Note: the controller for an unfiled report is "any" — the spec is
// per-controller, but since reports don't carry a controller field
// (they're filed by reporters, and the controller is the operator's
// controller looked up at resolve time), the cooldown is effectively
// per-(operator, serviceType) in practice. We key on
// (operator_controller, operator, service_type) and store the
// dismissed_at so future logic can support per-controller granularity.
//
// Design note: the cooldown is checked at MsgReportOperator time
// against the operator's CURRENT controller. If the controller has
// changed since the cooldown was recorded, the new controller is not
// blocked — this is intentional. §3.4.5's cooldown protects an
// operator from repeated AUTO_DISMISSED filings against the same
// controller-reporter pair; a controller swap legitimately resets the
// review chain. The dismissed_at key persists so future per-pair
// policy refinements can use the historical record.
func (k Keeper) isRefileCooldownActive(
	ctx context.Context,
	params types.Params,
	controllerBytes, opBytes []byte,
	serviceType string,
	currentHeight int64,
) (bool, error) {
	// RefileCooldowns keyed by (controller, op, service_type, dismissed_at).
	// Iterate by (controller, op, service_type) prefix using a Quad range
	// with K1=controller (Quad ranges only prefix on K1; filter K2/K3 in
	// loop). Small per-controller scan.
	rng := collections.NewPrefixedQuadRange[[]byte, []byte, string, int64](controllerBytes)
	iter, err := k.RefileCooldowns.Iterate(ctx, rng)
	if err != nil {
		return false, err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return false, err
		}
		if string(key.K2()) != string(opBytes) || key.K3() != serviceType {
			continue
		}
		dismissedAt := key.K4()
		if currentHeight < dismissedAt+params.ReportRefileCooldownBlocks {
			return true, nil
		}
		// Expired — lazy prune.
		_ = k.RefileCooldowns.Remove(ctx, key)
	}
	return false, nil
}

// recordRefileCooldown adds a cooldown entry for (controller, op,
// serviceType, dismissed_at). Called from the EndBlocker pending sweep
// when a PENDING report is AUTO_DISMISSED (§3.6 queue 2).
func (k Keeper) recordRefileCooldown(
	ctx context.Context,
	controllerStr, opStr, serviceType string,
	dismissedAt int64,
) error {
	controllerBytes, err := k.addrBytes(controllerStr)
	if err != nil {
		return err
	}
	opBytes, err := k.addrBytes(opStr)
	if err != nil {
		return err
	}
	return k.RefileCooldowns.Set(
		ctx,
		collections.Join4(controllerBytes, opBytes, serviceType, dismissedAt),
		types.RefileCooldown{
			Controller:      controllerStr,
			OperatorAddress: opStr,
			ServiceType:     serviceType,
			DismissedAt:     dismissedAt,
		},
	)
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
		op.Tier1WindowStartBond = op.Bond.Amount
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

// applySlashToBond debits slashAmount from op.Bond and flips status to
// UNDERFUNDED if the new bond falls below min_bond. Returns the new bond
// (post-debit). Caller is responsible for moving SPARK out of the bond
// pool (to Tier1Escrow for T1, or community pool for T2/dissolution)
// and for persisting the operator.
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

	newBondAmount := op.Bond.Amount.Sub(slashAmount)
	if newBondAmount.IsNegative() {
		// Shouldn't happen given computeSlashAmount is floor(bond * bps / 10000),
		// but defensive.
		newBondAmount = sdkmath.ZeroInt()
	}
	op.Bond = sdk.NewCoin(op.Bond.Denom, newBondAmount)

	// UNDERFUNDED transition: bond < min_bond AND status was ACTIVE.
	if op.Status == types.OperatorStatus_OPERATOR_STATUS_ACTIVE &&
		newBondAmount.LT(cfg.MinBond.Amount) {
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

// eagerReleaseExpiredEscrowsForOperator drops any Tier1Escrow rows for
// (op, serviceType) whose release_at has passed and that are linked to
// a terminally-resolved report, transferring the escrowed SPARK to the
// community pool inline. Called by MsgClaimUnbondedBond so an operator
// isn't blocked waiting for the EndBlocker sweep (§3.5).
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
		// Pay out escrowed amount to community pool. If distribution
		// keeper is wired; otherwise leave funds in module account
		// (standalone dev mode).
		if k.distributionKeeper() != nil && !p.escrow.Amount.Amount.IsZero() {
			moduleAddr := k.bankModuleAddress()
			if err := k.distributionKeeper().FundCommunityPool(ctx, sdk.NewCoins(p.escrow.Amount), moduleAddr); err != nil {
				return err
			}
		}
		if err := k.Tier1Escrow.Remove(ctx, p.escrowID); err != nil {
			return err
		}
		_ = k.Tier1EscrowByOperator.Remove(ctx, collections.Join3(opBytes, serviceType, p.escrowID))
		_ = k.Tier1EscrowReleaseQueue.Remove(ctx, collections.Join(p.escrow.ReleaseAt, p.escrowID))
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(types.NewTier1EscrowReleasedEvent(
			p.escrowID, p.escrow.ReportId, p.escrow.OperatorAddress, p.escrow.ServiceType,
			p.escrow.Amount, types.EscrowDestCommunityPool,
		))
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
		if !report.Deposit.Amount.IsNil() && !report.Deposit.Amount.IsZero() {
			reporterBytes, err := k.addrBytes(report.Reporter)
			if err == nil {
				if err := k.bankKeeper.SendCoinsFromModuleToAccount(
					ctx, types.ModuleName, sdk.AccAddress(reporterBytes), sdk.NewCoins(report.Deposit),
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
			_ = k.PendingReportsQueue.Remove(ctx, collections.Join(report.FiledAt, report.ReportId))
		case types.ReportStatus_REPORT_STATUS_ESCALATED:
			_ = k.EscalatedReportsQueue.Remove(ctx, collections.Join(report.EscalatedAt, report.ReportId))
		}

		// Mark CLOSED_OPERATOR_DISSOLVED.
		report.Status = types.ReportStatus_REPORT_STATUS_CLOSED_OPERATOR_DISSOLVED
		if err := k.Reports.Set(ctx, reportID, report); err != nil {
			return err
		}
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
			types.NewReportClosedDissolvedEvent(report.ReportId, report.Deposit),
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
