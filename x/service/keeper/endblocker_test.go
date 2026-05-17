package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
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
		Bond:                    sdk.NewCoin(types.BondDenom, math.NewInt(100_000)), // below min
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
		Bond:                    sdk.NewCoin(types.BondDenom, math.NewInt(100_000)),
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

// TestEndBlocker_EscalatedAutoTimeout flips an old ESCALATED report to
// AUTO_TIMEOUT and refunds the deposit.
func TestEndBlocker_EscalatedAutoTimeout(t *testing.T) {
	f := initFixture(t)
	reportID := f.seedEscalatedReport(t, 200)

	params, _ := f.keeper.Params.Get(f.ctx)
	f.withBlockHeight(f.sdkCtx().BlockHeight() + params.MaxEscalatedBlocks + 1)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))

	r, err := f.keeper.Reports.Get(f.ctx, reportID)
	require.NoError(t, err)
	require.Equal(t, types.ReportStatus_REPORT_STATUS_AUTO_TIMEOUT, r.Status)
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
}

// TestEndBlocker_RefileCooldownRecordedOnAutoDismiss ensures that
// auto-dismissed reports cause subsequent reports to hit the refile
// cooldown.
func TestEndBlocker_RefileCooldownRecordedOnAutoDismiss(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	f.seedPendingReport(t)

	params, _ := f.keeper.Params.Get(f.ctx)
	f.withBlockHeight(f.sdkCtx().BlockHeight() + params.MaxPendingBlocks + 1)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))

	// New reporter (sidestep rate-limit). Should be rejected by refile
	// cooldown.
	_, err := f.msgServer.ReportOperator(f.ctx, &types.MsgReportOperator{
		Reporter:    testRandom,
		Operator:    testOperator1,
		ServiceType: testServiceType,
		Reason:      "re-filing",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrRefileCooldownActive.Error())
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
			Bond:                    sdk.NewCoin(types.BondDenom, math.NewInt(100_000)),
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
