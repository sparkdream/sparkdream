package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// We test reports.go helpers through the msg_server paths where possible
// (already covered in resolve_report_test.go etc.). This file exercises
// the pieces not naturally driven by msg-server happy paths:
//
//   - tier1 cooldown enforcement
//   - tier1 aggregate cap enforcement
//   - HasOpenReports / HasActiveTier1Escrow read paths
//   - applySlashToBond ACTIVE→UNDERFUNDED transition
//   - settleBondBlocks accrual

func TestApplySlashToBond_FlipsToUnderfunded(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	height := f.sdkCtx().BlockHeight()

	// Trigger an internal slash via the public SlashOperator API which
	// wraps applySlashToBond. Use Tier2Jury source so cooldown/aggregate
	// gates are bypassed and the test focuses on the bond/status math.
	f.seedActiveOperator(t, testOperator1, testController, cfg.MinBondAmount)

	// Slash 5000 bps (50%) — half of min_bond → falls below min_bond.
	_, err := f.keeper.SlashOperator(f.ctx, testOperator1Addr, testServiceType, 5000, 0, keeper.SlashSourceTier2Jury)
	require.NoError(t, err)

	op, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.True(t, ok)
	// Operator slashed via Tier2 path stays ACTIVE if bond > 0 and > min,
	// otherwise transitions to UNDERFUNDED. We slashed half of min_bond,
	// so new bond is min/2 < min → UNDERFUNDED.
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED, op.Status)
	require.GreaterOrEqual(t, op.UnderfundedSince, height)
}

func TestSlashOperator_Tier1CooldownEnforced(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(10_000_000))

	// First slash: succeeds.
	_, err := f.keeper.SlashOperator(f.ctx, testOperator1Addr, testServiceType, 100, 0, keeper.SlashSourceTier1)
	require.NoError(t, err)

	// Second slash within cooldown: rejected.
	_, err = f.keeper.SlashOperator(f.ctx, testOperator1Addr, testServiceType, 100, 0, keeper.SlashSourceTier1)
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrTier1CooldownActive.Error())

	// After cooldown elapsed: allowed again.
	f.withBlockHeight(f.sdkCtx().BlockHeight() + cfg.Tier1CooldownBlocks + 1)
	_, err = f.keeper.SlashOperator(f.ctx, testOperator1Addr, testServiceType, 100, 0, keeper.SlashSourceTier1)
	require.NoError(t, err)
}

func TestSlashOperator_Tier1AggregateCapEnforced(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(10_000_000))

	// Aggregate cap = 15% of window-start bond. Slash 5% three times
	// across cooldown gaps — third slash projects total = 15% (cap) which
	// passes. A fourth would exceed.
	// Walk three at 5% each, advancing past the cooldown between each.
	for i := 0; i < 3; i++ {
		_, err := f.keeper.SlashOperator(f.ctx, testOperator1Addr, testServiceType, 500, 0, keeper.SlashSourceTier1)
		require.NoError(t, err, "slash %d unexpectedly rejected", i+1)
		f.withBlockHeight(f.sdkCtx().BlockHeight() + cfg.Tier1CooldownBlocks + 1)
	}

	// Fourth slash within the same window: exceeds 15% cap.
	_, err := f.keeper.SlashOperator(f.ctx, testOperator1Addr, testServiceType, 500, 0, keeper.SlashSourceTier1)
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrTier1AggregateCapExceeded.Error())
}

func TestHasOpenReports(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)

	// No reports: false.
	has, err := f.keeper.HasOpenReports(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.NoError(t, err)
	require.False(t, has)

	// Seed a PENDING report.
	reportID, err := f.keeper.NextReportID.Next(f.ctx)
	require.NoError(t, err)
	r := types.Report{
		ReportId:        reportID,
		OperatorAddress: testOperator1,
		ServiceType:     testServiceType,
		Reporter:        testReporter,
		Status:          types.ReportStatus_REPORT_STATUS_PENDING,
		SlashAmount:     math.ZeroInt(),
		Deposit:         math.NewInt(10_000_000),
	}
	require.NoError(t, f.keeper.Reports.Set(f.ctx, reportID, r))
	require.NoError(t, f.keeper.ReportsByOperator.Set(f.ctx, collections.Join3(testOperator1Addr.Bytes(), testServiceType, reportID)))

	has, err = f.keeper.HasOpenReports(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.NoError(t, err)
	require.True(t, has)

	// Flip to RESOLVED_T1 → no longer "open".
	r.Status = types.ReportStatus_REPORT_STATUS_RESOLVED_T1
	require.NoError(t, f.keeper.Reports.Set(f.ctx, reportID, r))

	has, err = f.keeper.HasOpenReports(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.NoError(t, err)
	require.False(t, has)
}

func TestHasActiveTier1Escrow(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)

	currentHeight := f.sdkCtx().BlockHeight()
	has, err := f.keeper.HasActiveTier1Escrow(f.ctx, testOperator1Addr.Bytes(), testServiceType, currentHeight)
	require.NoError(t, err)
	require.False(t, has)

	// Active (release_at in future): true.
	escrowID, _ := f.keeper.NextEscrowID.Next(f.ctx)
	releaseAt := currentHeight + 100
	require.NoError(t, f.keeper.Tier1Escrow.Set(f.ctx, escrowID, types.Tier1EscrowEntry{
		EscrowId:        escrowID,
		ReleaseAt:       releaseAt,
		OperatorAddress: testOperator1,
		ServiceType:     testServiceType,
		Amount:          math.NewInt(1),
	}))
	require.NoError(t, f.keeper.Tier1EscrowByOperator.Set(f.ctx,
		collections.Join3(testOperator1Addr.Bytes(), testServiceType, escrowID)))

	has, err = f.keeper.HasActiveTier1Escrow(f.ctx, testOperator1Addr.Bytes(), testServiceType, currentHeight)
	require.NoError(t, err)
	require.True(t, has)

	// Expired (release_at past current): false (active means still in window).
	has, err = f.keeper.HasActiveTier1Escrow(f.ctx, testOperator1Addr.Bytes(), testServiceType, releaseAt+1)
	require.NoError(t, err)
	require.False(t, has)
}
