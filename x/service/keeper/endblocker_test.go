package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

// TestEndBlocker_UnderfundedForceUnbond seeds an UNDERFUNDED operator
// past its grace period and confirms EndBlocker flips it to UNBONDING.
func TestEndBlocker_UnderfundedForceUnbond(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	height := f.sdkCtx().BlockHeight()

	op := types.Operator{
		Address:                 testOperator1,
		ServiceType:             testServiceType,
		Controller:              testController,
		BondAmount:              math.NewInt(100_000), // below min
		Status:                  types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED,
		UnderfundedSince:        height,
		Tier1SlashedInWindow:    math.ZeroInt(),
		Tier1WindowStart:        height,
		Tier1WindowStartBond:    math.NewInt(100_000),
		RegisteredAt:            height,
		TotalLifetimeBondBlocks: math.ZeroInt(),
		LastBondBlockUpdateAt:   height,
	}
	require.NoError(t, f.keeper.PutOperator(f.ctx, op))

	// Advance past grace period.
	f.withBlockHeight(height + cfg.UnderfundedGraceBlocks + 1)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))

	post, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.True(t, ok)
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_UNBONDING, post.Status)
	require.EqualValues(t, 0, post.UnderfundedSince)

	// UnderfundedQueue entry cleaned up.
	has, _ := f.keeper.UnderfundedQueue.Has(f.ctx, collections.Join3(height, testOperator1Addr.Bytes(), testServiceType))
	require.False(t, has)
}

// TestEndBlocker_UnderfundedWithinGrace leaves an under-grace operator
// alone.
func TestEndBlocker_UnderfundedWithinGrace(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	height := f.sdkCtx().BlockHeight()

	op := types.Operator{
		Address:                 testOperator1,
		ServiceType:             testServiceType,
		Controller:              testController,
		BondAmount:              math.NewInt(100_000),
		Status:                  types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED,
		UnderfundedSince:        height,
		Tier1SlashedInWindow:    math.ZeroInt(),
		Tier1WindowStart:        height,
		Tier1WindowStartBond:    math.NewInt(100_000),
		RegisteredAt:            height,
		TotalLifetimeBondBlocks: math.ZeroInt(),
		LastBondBlockUpdateAt:   height,
	}
	require.NoError(t, f.keeper.PutOperator(f.ctx, op))

	f.withBlockHeight(height + cfg.UnderfundedGraceBlocks - 1)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))

	post, _ := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED, post.Status)
}

// TestEndBlocker_PendingAutoDismiss flips an old PENDING report to
// AUTO_DISMISSED and refunds the deposit.
func TestEndBlocker_PendingAutoDismiss(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	reportID := f.seedPendingReport(t)

	params, _ := f.keeper.Params.Get(f.ctx)
	f.withBlockHeight(f.sdkCtx().BlockHeight() + params.MaxPendingBlocks + 1)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))

	r, err := f.keeper.Reports.Get(f.ctx, reportID)
	require.NoError(t, err)
	require.Equal(t, types.ReportStatus_REPORT_STATUS_AUTO_DISMISSED, r.Status)

	// Reporter deposit refunded.
	require.Len(t, f.bankKeeper.ModToAcctCalls, 1)
	require.Equal(t, testReporterAddr, f.bankKeeper.ModToAcctCalls[0].Recipient)
}

// TestEndBlocker_PendingNotExpiredStays leaves a fresh PENDING report
// alone.
func TestEndBlocker_PendingNotExpiredStays(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	reportID := f.seedPendingReport(t)

	require.NoError(t, f.keeper.EndBlocker(f.ctx))

	r, err := f.keeper.Reports.Get(f.ctx, reportID)
	require.NoError(t, err)
	require.Equal(t, types.ReportStatus_REPORT_STATUS_PENDING, r.Status)
}

// TestEndBlocker_EscalatedAutoTimeout flips an old ESCALATED report whose
// jury did not uphold it to AUTO_TIMEOUT and refunds the deposit.
func TestEndBlocker_EscalatedAutoTimeout(t *testing.T) {
	f := initFixture(t)
	reportID := f.seedEscalatedReport(t, 200)
	f.repKeeper.GetJuryVerdictNameFn = func(context.Context, uint64) (string, error) {
		return types.JuryVerdictInconclusive, nil
	}

	params, _ := f.keeper.Params.Get(f.ctx)
	f.withBlockHeight(f.sdkCtx().BlockHeight() + params.MaxEscalatedBlocks + 1)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))

	r, err := f.keeper.Reports.Get(f.ctx, reportID)
	require.NoError(t, err)
	require.Equal(t, types.ReportStatus_REPORT_STATUS_AUTO_TIMEOUT, r.Status)
}

// SVC-101: an upheld verdict nobody relays is applied at the deadline as
// ACCEPT at the proposed slash, without dissolving the operator.
func TestEndBlocker_EscalatedUpheldVerdictApplied(t *testing.T) {
	f := initFixture(t)
	reportID := f.seedEscalatedReport(t, 200)
	pre, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.True(t, ok)

	params, _ := f.keeper.Params.Get(f.ctx)
	f.withBlockHeight(f.sdkCtx().BlockHeight() + params.MaxEscalatedBlocks + 1)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))

	r, err := f.keeper.Reports.Get(f.ctx, reportID)
	require.NoError(t, err)
	require.Equal(t, types.ReportStatus_REPORT_STATUS_RESOLVED_T2, r.Status)
	require.Equal(t, pre.BondAmount.MulRaw(200).QuoRaw(10000).String(), r.SlashAmount.String())

	post, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.True(t, ok)
	require.Equal(t, pre.BondAmount.Sub(r.SlashAmount).String(), post.BondAmount.String())
	require.Empty(t, f.hooks.Dissolved)
	has, err := f.keeper.EscalatedReportsQueue.Has(f.ctx, collections.Join(r.EscalatedAt, reportID))
	require.NoError(t, err)
	require.False(t, has)
}

// SVC-105: a record whose processing fails is logged and skipped, stays
// queued, and does not stop the EndBlocker or the other records.
func TestEndBlocker_FailingItemIsSkippedNotFatal(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	failing := f.seedPendingReport(t)

	// the deposit refund fails: the item must roll back, not halt
	f.bankKeeper.SendCoinsFromModuleToAccountFn = func(context.Context, string, sdk.AccAddress, sdk.Coins) error {
		return types.ErrInvalidVerdict.Wrap("bank down")
	}
	params, _ := f.keeper.Params.Get(f.ctx)
	f.withBlockHeight(f.sdkCtx().BlockHeight() + params.MaxPendingBlocks + 1)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))

	r, err := f.keeper.Reports.Get(f.ctx, failing)
	require.NoError(t, err)
	require.Equal(t, types.ReportStatus_REPORT_STATUS_PENDING, r.Status)

	// once the bank recovers, the queued record is processed
	f.bankKeeper.SendCoinsFromModuleToAccountFn = nil
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	r, err = f.keeper.Reports.Get(f.ctx, failing)
	require.NoError(t, err)
	require.Equal(t, types.ReportStatus_REPORT_STATUS_AUTO_DISMISSED, r.Status)
}

// TestEndBlocker_Tier1EscrowReleaseToCommunityPool simulates an expired
// (uncontested) Tier1Escrow and confirms it's swept to the community pool.
func TestEndBlocker_Tier1EscrowReleaseToCommunityPool(t *testing.T) {
	f := initFixture(t)
	reportID := f.seedResolvedT1ReportAndSlash(t, 200)

	// Locate the live escrow entry for this report.
	escrowID, ok := findEscrowFor(t, f, reportID)
	require.True(t, ok)
	escrow, err := f.keeper.Tier1Escrow.Get(f.ctx, escrowID)
	require.NoError(t, err)

	// Advance past release_at.
	f.withBlockHeight(escrow.ReleaseAt + 1)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))

	// Escrow row removed.
	_, err = f.keeper.Tier1Escrow.Get(f.ctx, escrowID)
	require.Error(t, err)

	// Distribution keeper received the slashed amount.
	require.NotEmpty(t, f.distributionKeeper.Calls)

	// The tier-1 slash is final once released: service-operator reputation
	// is deducted in proportion to the share of the bond taken (§6.6).
	require.Len(t, f.repKeeper.DeductReputationCalls, 1)
	ded := f.repKeeper.DeductReputationCalls[0]
	require.Equal(t, types.ReputationTagServiceOperator, ded.Tag)
	require.True(t, ded.Amount.IsPositive())
	op, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.True(t, ok)
	require.Equal(t, escrow.OperatorAddress, op.Address)
}

// TestEndBlocker_RefileCooldownRecordedOnAutoDismiss ensures that an
// auto-dismissed member report bars ITS reporter from re-filing (SVC-01:
// the cooldown is per reporter), while other members can still report.
func TestEndBlocker_RefileCooldownRecordedOnAutoDismiss(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	f.seedPendingReport(t)

	params, _ := f.keeper.Params.Get(f.ctx)
	f.withBlockHeight(f.sdkCtx().BlockHeight() + params.MaxPendingBlocks + 1)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))

	// The reporter whose allegation lapsed cannot re-file it.
	_, err := f.msgServer.ReportOperator(f.ctx, &types.MsgReportOperator{
		Reporter:    testReporter,
		Operator:    testOperator1,
		ServiceType: testServiceType,
		Reason:      "re-filing",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrRefileCooldownActive.Error())

	// Anyone else still can.
	_, err = f.msgServer.ReportOperator(f.ctx, &types.MsgReportOperator{
		Reporter:    testRandom,
		Operator:    testOperator1,
		ServiceType: testServiceType,
		Reason:      "independent report",
	})
	require.NoError(t, err)
}

// SVC-01: an auto-dismissed SYSTEM report (zero deposit — a federation
// challenge or a liveness report) records no refile cooldown, so it never
// locks members out of reporting the operator.
func TestEndBlocker_SystemReportAutoDismissNoCooldown(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	reportID := f.seedPendingReport(t)
	r, _ := f.keeper.Reports.Get(f.ctx, reportID)
	r.Deposit = math.ZeroInt()
	require.NoError(t, f.keeper.Reports.Set(f.ctx, reportID, r))

	params, _ := f.keeper.Params.Get(f.ctx)
	f.withBlockHeight(f.sdkCtx().BlockHeight() + params.MaxPendingBlocks + 1)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))

	n := 0
	require.NoError(t, f.keeper.RefileCooldowns.Walk(f.ctx, nil, func(collections.Quad[[]byte, []byte, string, int64], types.RefileCooldown) (bool, error) {
		n++
		return false, nil
	}))
	require.Zero(t, n)
	_, err := f.msgServer.ReportOperator(f.ctx, &types.MsgReportOperator{
		Reporter: testRandom, Operator: testOperator1, ServiceType: testServiceType, Reason: "member report",
	})
	require.NoError(t, err)
}

// TestEndBlocker_SweepLimitCaps tests that the per-sweep limit caps how
// many records are processed in a single block.
func TestEndBlocker_SweepLimitCaps(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	height := f.sdkCtx().BlockHeight()

	// Set a tiny sweep limit so we can verify the cap is honored.
	params, _ := f.keeper.Params.Get(f.ctx)
	params.EndblockerSweepLimit = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Seed two UNDERFUNDED operators.
	for i, addr := range []string{testOperator1, testOperator2} {
		var addrBytes []byte
		if i == 0 {
			addrBytes = testOperator1Addr.Bytes()
		} else {
			addrBytes = testOperator2Addr.Bytes()
		}
		op := types.Operator{
			Address:                 addr,
			ServiceType:             testServiceType,
			Controller:              testController,
			BondAmount:              math.NewInt(100_000),
			Status:                  types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED,
			UnderfundedSince:        height,
			Tier1SlashedInWindow:    math.ZeroInt(),
			Tier1WindowStart:        height,
			Tier1WindowStartBond:    math.NewInt(100_000),
			RegisteredAt:            height,
			TotalLifetimeBondBlocks: math.ZeroInt(),
			LastBondBlockUpdateAt:   height,
		}
		require.NoError(t, f.keeper.PutOperator(f.ctx, op))
		_ = addrBytes
	}

	f.withBlockHeight(height + cfg.UnderfundedGraceBlocks + 1)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))

	// Exactly one operator transitioned; the other remains UNDERFUNDED.
	op1, _ := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	op2, _ := f.keeper.GetOperator(f.ctx, testOperator2Addr.Bytes(), testServiceType)
	statuses := []types.OperatorStatus{op1.Status, op2.Status}
	unbondingCount := 0
	underfundedCount := 0
	for _, s := range statuses {
		switch s {
		case types.OperatorStatus_OPERATOR_STATUS_UNBONDING:
			unbondingCount++
		case types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED:
			underfundedCount++
		}
	}
	require.Equal(t, 1, unbondingCount)
	require.Equal(t, 1, underfundedCount)
}

// ---------------------------------------------------------------------------
// ReportTimeoutAction (Phase 0 federation→service migration):
// PENDING reports past max_pending_blocks either auto-DISMISS (default)
// or auto-ESCALATE to a jury case, depending on the per-service-type
// config knob.
// ---------------------------------------------------------------------------

func TestEndBlocker_ReportTimeoutActionEscalate(t *testing.T) {
	f := initFixture(t)

	cfg := f.seedServiceType(t)
	cfg.ReportTimeoutAction = types.ReportTimeoutAction_REPORT_TIMEOUT_ACTION_ESCALATE
	cfg.ChallengeDefaultSlashBps = 200
	require.NoError(t, f.keeper.ServiceTypes.Set(f.ctx, cfg.ServiceType, cfg))
	f.seedActiveOperator(t, testOperator1, testController, cfg.MinBondAmount.MulRaw(10))

	fedAddr := authtypes.NewModuleAddress(types.FederationModuleName)
	reportID, _, err := f.keeper.OpenSystemReport(
		f.ctx, fedAddr, testOperator1Addr,
		testServiceType, 0, "ipfs://Qm-evidence", []byte("escalate-me"),
	)
	require.NoError(t, err)

	// Advance past max_pending_blocks.
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	report, err := f.keeper.Reports.Get(f.ctx, reportID)
	require.NoError(t, err)
	f.withBlockHeight(report.FiledAt + params.MaxPendingBlocks + 1)

	// EndBlocker. The PENDING report should now be ESCALATED with a
	// jury case opened.
	require.NoError(t, f.keeper.EndBlocker(f.ctx))

	post, err := f.keeper.Reports.Get(f.ctx, reportID)
	require.NoError(t, err)
	require.Equal(t, types.ReportStatus_REPORT_STATUS_ESCALATED, post.Status)
	require.NotZero(t, post.JuryCaseId, "jury case should be opened")
	require.EqualValues(t, cfg.ChallengeDefaultSlashBps, post.ProposedSlashBps)
	require.Equal(t, f.sdkCtx().BlockHeight(), post.EscalatedAt)
}

func TestEndBlocker_ReportTimeoutActionDismissDefault(t *testing.T) {
	// Sanity: with the default DISMISS action, timed-out reports
	// auto-dismiss as before. Regression guard against Phase 0
	// accidentally changing the default behavior.
	f := initFixture(t)

	cfg := f.seedServiceType(t)
	cfg.ReportTimeoutAction = types.ReportTimeoutAction_REPORT_TIMEOUT_ACTION_DISMISS
	require.NoError(t, f.keeper.ServiceTypes.Set(f.ctx, cfg.ServiceType, cfg))
	f.seedActiveOperator(t, testOperator1, testController, cfg.MinBondAmount.MulRaw(10))

	// Use MsgReportOperator path (not OpenSystemReport) so the report
	// has a real reporter and deposit.
	reportID := f.fileMemberReport(t)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	report, err := f.keeper.Reports.Get(f.ctx, reportID)
	require.NoError(t, err)
	f.withBlockHeight(report.FiledAt + params.MaxPendingBlocks + 1)

	require.NoError(t, f.keeper.EndBlocker(f.ctx))

	post, err := f.keeper.Reports.Get(f.ctx, reportID)
	require.NoError(t, err)
	require.Equal(t, types.ReportStatus_REPORT_STATUS_AUTO_DISMISSED, post.Status)
}

// fileMemberReport seeds a MsgReportOperator-style report against
// testOperator1 from testReporter (helper used by DISMISS regression test).
func (f *fixture) fileMemberReport(t *testing.T) uint64 {
	t.Helper()
	// Ensure repKeeper doesn't gate on trust level.
	f.repKeeper.MeetsTrustLevelFn = func(_ context.Context, _ sdk.AccAddress, _ string) (bool, error) {
		return true, nil
	}
	// Reporter must NOT be a controller member.
	f.commonsKeeper.IsGroupPolicyMemberFn = func(_ context.Context, _ string, _ string) (bool, error) {
		return false, nil
	}
	resp, err := f.msgServer.ReportOperator(f.ctx, &types.MsgReportOperator{
		Reporter:    testReporter,
		Operator:    testOperator1,
		ServiceType: testServiceType,
		Reason:      "test-evidence",
	})
	require.NoError(t, err)
	require.NotZero(t, resp.ReportId)
	return resp.ReportId
}

// SVC-202: a panic inside one EndBlocker item is recovered like an error:
// the item rolls back and stays queued, FinalizeBlock does not fail.
func TestEndBlocker_PanickingItemIsSkippedNotFatal(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	stuck := f.seedPendingReport(t)

	f.bankKeeper.SendCoinsFromModuleToAccountFn = func(context.Context, string, sdk.AccAddress, sdk.Coins) error {
		panic("bank keeper unwired")
	}
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	f.withBlockHeight(f.sdkCtx().BlockHeight() + params.MaxPendingBlocks + 1)
	require.NotPanics(t, func() { require.NoError(t, f.keeper.EndBlocker(f.ctx)) })

	r, err := f.keeper.Reports.Get(f.ctx, stuck)
	require.NoError(t, err)
	require.Equal(t, types.ReportStatus_REPORT_STATUS_PENDING, r.Status, "the panicking item rolled back")

	f.bankKeeper.SendCoinsFromModuleToAccountFn = nil
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	r, err = f.keeper.Reports.Get(f.ctx, stuck)
	require.NoError(t, err)
	require.Equal(t, types.ReportStatus_REPORT_STATUS_AUTO_DISMISSED, r.Status, "retried once the panic is gone")
}

// SVC-202: out-of-gas is the block gas meter's decision and is re-raised,
// never swallowed; every other panic is contained and the item not counted.
func TestRunIsolated_RecoversPanicsButNotOutOfGas(t *testing.T) {
	f := initFixture(t)
	counted := f.keeper.RunIsolatedForTest(f.ctx, "test", func(context.Context) (bool, error) {
		panic("boom")
	})
	require.False(t, counted)

	require.Panics(t, func() {
		f.keeper.RunIsolatedForTest(f.ctx, "test", func(context.Context) (bool, error) {
			panic(storetypes.ErrorOutOfGas{Descriptor: "test"})
		})
	})

	ok := f.keeper.RunIsolatedForTest(f.ctx, "test", func(context.Context) (bool, error) { return true, nil })
	require.True(t, ok)
}
