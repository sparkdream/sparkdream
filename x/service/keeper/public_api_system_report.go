package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/service/types"
)

// allowedSystemCallers lists the module-account names allowed to call
// OpenSystemReport. Sorted slice (NOT a map) so iteration order is
// deterministic across nodes — Go map iteration is randomized, which
// would risk a consensus break if "first match wins" returned different
// results on different nodes once the list grows past one entry.
//
// Adding a new caller requires a source patch (intentional privilege-
// gate friction); the in-tree caller has to be wired with a
// ServiceKeeper reference anyway, so the allowlist + auth-keeper lookup
// is defense-in-depth + auditability, not the primary authorization
// barrier. Real authorization is keeper-wiring discipline.
var allowedSystemCallers = []string{
	types.FederationModuleName,
}

// resolveSystemCaller forward-derives each allowed module's account
// address and compares to the submitted callerModuleAddr. Returns the
// matched module name on success, "" + ErrUnauthorizedSystemCaller on
// failure. The auth keeper's GetModuleAddress is the canonical SDK
// pattern (cosmos-sdk doesn't expose a reverse address→name lookup).
func (k Keeper) resolveSystemCaller(callerModuleAddr sdk.AccAddress) (string, error) {
	if k.authKeeper == nil {
		// Standalone mode (early dev / unit tests with no auth keeper
		// wired). Reject — OpenSystemReport without a wired auth keeper
		// is unsafe.
		return "", types.ErrUnauthorizedSystemCaller.Wrap("auth keeper not wired")
	}
	for _, name := range allowedSystemCallers {
		expected := k.authKeeper.GetModuleAddress(name)
		if expected != nil && expected.Equals(callerModuleAddr) {
			return name, nil
		}
	}
	return "", types.ErrUnauthorizedSystemCaller.Wrapf("address %s does not match any allowed module account", callerModuleAddr)
}

// OpenSystemReport files a PENDING report from an allowlisted caller
// module (e.g. x/federation on challenge-quorum resolution) without
// requiring a member reporter, a trust-level check, or a SPARK deposit.
// The caller-module account is recorded as the report's reporter for
// audit purposes; the deposit field is zero.
//
// Idempotency: (matchedModule, dedupeKey) is tracked in SystemReportDedup.
// A second call with the same key during the existing report's lifetime
// returns the existing report_id without opening a new one. Once the
// existing report transitions to a terminal state (AUTO_DISMISSED,
// RESOLVED_T1, RESOLVED_T2, AUTO_TIMEOUT, CLOSED_DISSOLVED), the dedupe
// entry is left in place but the keeper will only return that report_id
// — callers wanting to re-file after a dismissal must use a fresh
// dedupe_key (typically by including the source challenge_id so a fresh
// challenge produces a fresh key).
//
// Rate limiting: per-(matchedModule) sliding-window ring buffer. Each
// fresh report counts against `max_system_reports_per_caller_per_window`
// over `rate_limit_window_blocks`. Idempotent re-calls don't count.
// Exceeding the cap returns ErrSystemReportRateLimited and emits
// system_report_rate_limited for off-chain alerting.
//
// Slash amount: if slashBps == 0 the keeper falls back to the service
// type config's challenge_default_slash_bps; otherwise the caller-
// provided value is used (still bounded by the controller at resolve
// time within unilateral_slash_cap_bps). slashBps is recorded on
// Report.ProposedSlashBps so the controller sees the caller's proposal
// at resolve time.
//
// Emits:
//   - service.system_report_opened (with idempotent=true on re-calls)
//   - service.system_report_rate_limited on cap exceeded (no other
//     state changes).
//
// Used by x/federation's content-challenge resolution path (Phase 6 of
// the federation→service migration plan).
func (k Keeper) OpenSystemReport(
	ctx context.Context,
	callerModuleAddr sdk.AccAddress,
	operatorAddr sdk.AccAddress,
	serviceType string,
	slashBps uint32,
	evidenceURI string,
	dedupeKey []byte,
) (reportID uint64, idempotent bool, err error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	// 1. Authorize caller.
	matchedModule, err := k.resolveSystemCaller(callerModuleAddr)
	if err != nil {
		return 0, false, err
	}

	if len(dedupeKey) == 0 {
		return 0, false, types.ErrInvalidDedupeKey
	}

	// 2. Idempotency check: (matchedModule, dedupeKey) → existing report.
	dedupePK := collections.Join(matchedModule, dedupeKey)
	if existing, err := k.SystemReportDedup.Get(ctx, dedupePK); err == nil {
		// Already filed. Don't update rate limit; don't allocate new
		// report_id; emit the event with idempotent=true so observers
		// can distinguish retries.
		opStr, _ := k.addressCodec.BytesToString(operatorAddr.Bytes())
		sdkCtx.EventManager().EmitEvent(types.NewSystemReportOpenedEvent(
			existing, matchedModule, opStr, serviceType, evidenceURI, dedupeKey, true,
		))
		return existing, true, nil
	}

	// 3. Validate operator exists in the live store (we don't slash
	//    archived records). Service-type-disabled operators can still be
	//    reported — disabling a service type doesn't bypass accountability
	//    for already-registered operators.
	opBytes := operatorAddr.Bytes()
	op, exists := k.GetOperator(ctx, opBytes, serviceType)
	if !exists {
		opStr, _ := k.addressCodec.BytesToString(opBytes)
		return 0, false, types.ErrOperatorNotFound.Wrapf("%s / %s", opStr, serviceType)
	}

	// 4. Resolve effective slashBps.
	cfg, err := k.resolveServiceTypeConfig(ctx, serviceType)
	if err != nil {
		return 0, false, err
	}
	effectiveSlashBps := slashBps
	if effectiveSlashBps == 0 {
		effectiveSlashBps = cfg.ChallengeDefaultSlashBps
	}
	if effectiveSlashBps == 0 {
		return 0, false, types.ErrSlashCapExceeded.Wrap("no slash_bps provided and no challenge_default_slash_bps configured")
	}
	if effectiveSlashBps > cfg.UnilateralSlashCapBps {
		// Cap-bound the proposed slash. The controller can still adjust
		// downward at resolve time; the cap exists so a buggy caller
		// can't propose a stratospheric slash.
		effectiveSlashBps = cfg.UnilateralSlashCapBps
	}

	// 5. Rate limit check.
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, false, err
	}
	if err := k.checkSystemReportRateLimit(ctx, params, matchedModule, currentHeight); err != nil {
		opStr, _ := k.addressCodec.BytesToString(opBytes)
		sdkCtx.EventManager().EmitEvent(types.NewSystemReportRateLimitedEvent(
			matchedModule, opStr, serviceType,
		))
		return 0, false, err
	}

	// 6. Allocate report_id.
	reportID, err = k.NextReportID.Next(ctx)
	if err != nil {
		return 0, false, err
	}
	if reportID == 0 {
		reportID, err = k.NextReportID.Next(ctx)
		if err != nil {
			return 0, false, err
		}
	}

	// 7. Write the Report.
	reporterAddrStr, err := k.addressCodec.BytesToString(callerModuleAddr.Bytes())
	if err != nil {
		return 0, false, err
	}
	opAddrStr, err := k.addressCodec.BytesToString(opBytes)
	if err != nil {
		return 0, false, err
	}
	// Reason is the evidence URI prefixed with the caller-module marker
	// so audits can quickly distinguish system reports from member
	// reports without joining against dedupe state.
	reason := fmt.Sprintf("system:%s:%s", matchedModule, evidenceURI)
	if uint32(len(reason)) > params.MaxReasonBytes {
		// Truncate rather than reject — evidence URIs from external
		// systems can be long, and a system report shouldn't get killed
		// by a length boundary it doesn't control.
		reason = reason[:params.MaxReasonBytes]
	}

	report := types.Report{
		ReportId:         reportID,
		OperatorAddress:  opAddrStr,
		ServiceType:      serviceType,
		Reporter:         reporterAddrStr,
		Reason:           reason,
		FiledAt:          currentHeight,
		EscalatedAt:      0,
		Status:           types.ReportStatus_REPORT_STATUS_PENDING,
		ProposedSlashBps: effectiveSlashBps,
		SlashAmount:      sdkmath.ZeroInt(),
		Deposit:          sdkmath.ZeroInt(),
		JuryCaseId:       0,
	}
	if err := k.Reports.Set(ctx, reportID, report); err != nil {
		return 0, false, err
	}
	if err := k.ReportsByOperator.Set(ctx, collections.Join3(opBytes, serviceType, reportID)); err != nil {
		return 0, false, err
	}
	if err := k.PendingReportsQueue.Set(ctx, collections.Join(currentHeight, reportID)); err != nil {
		return 0, false, err
	}

	// 8. Record idempotency entry + rate-limit usage.
	if err := k.SystemReportDedup.Set(ctx, dedupePK, reportID); err != nil {
		return 0, false, err
	}
	if err := k.recordSystemReportFiling(ctx, params, matchedModule, currentHeight); err != nil {
		return 0, false, err
	}

	sdkCtx.EventManager().EmitEvent(types.NewSystemReportOpenedEvent(
		reportID, matchedModule, opAddrStr, serviceType, evidenceURI, dedupeKey, false,
	))
	// Reuse the report_filed metric for system reports too; the
	// caller_module attribute on the event distinguishes the source.
	emitReportFiled(serviceType)
	_ = op // keep op for forward-compat in case we add per-status gates

	return reportID, false, nil
}

// checkSystemReportRateLimit enforces the per-caller sliding-window cap
// from Phase 0. Mirror of checkReporterRateLimit but keyed on the
// caller module rather than a (reporter, operator) tuple.
func (k Keeper) checkSystemReportRateLimit(
	ctx context.Context,
	params types.Params,
	callerModule string,
	currentHeight int64,
) error {
	if params.MaxSystemReportsPerCallerPerWindow == 0 {
		// Treat zero as "unlimited" — params validation rejects this
		// unless explicitly opted-in via an explicit zero default.
		return nil
	}
	rl, err := k.SystemReportRateLimit.Get(ctx, callerModule)
	if err != nil {
		// First-time caller: nothing recorded yet.
		return nil
	}
	cutoff := currentHeight - params.RateLimitWindowBlocks
	inWindow := uint32(0)
	for _, h := range rl.RecentFilingHeights {
		if h >= cutoff {
			inWindow++
		}
	}
	if inWindow >= params.MaxSystemReportsPerCallerPerWindow {
		return types.ErrSystemReportRateLimited.Wrapf(
			"caller=%s in_window=%d cap=%d", callerModule, inWindow, params.MaxSystemReportsPerCallerPerWindow,
		)
	}
	return nil
}

// recordSystemReportFiling updates the per-caller ring buffer after a
// successful (non-idempotent) OpenSystemReport. Capped at cap+1 entries
// to absorb a brief overshoot during cap-edge calls.
func (k Keeper) recordSystemReportFiling(
	ctx context.Context,
	params types.Params,
	callerModule string,
	currentHeight int64,
) error {
	rl, err := k.SystemReportRateLimit.Get(ctx, callerModule)
	if err != nil {
		rl = types.SystemReportRateLimit{
			CallerModule:        callerModule,
			RecentFilingHeights: []int64{},
		}
	}
	rl.RecentFilingHeights = append(rl.RecentFilingHeights, currentHeight)
	// Trim oldest entries beyond cap + 1.
	maxLen := int(params.MaxSystemReportsPerCallerPerWindow) + 1
	if maxLen > 0 && len(rl.RecentFilingHeights) > maxLen {
		rl.RecentFilingHeights = rl.RecentFilingHeights[len(rl.RecentFilingHeights)-maxLen:]
	}
	return k.SystemReportRateLimit.Set(ctx, callerModule, rl)
}
