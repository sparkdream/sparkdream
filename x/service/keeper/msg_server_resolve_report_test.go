package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

// seedPendingReport creates a PENDING report owned by the given reporter
// against testOperator1/testServiceType. Returns the report id.
func (f *fixture) seedPendingReport(t *testing.T) uint64 {
	t.Helper()
	reportID, err := f.keeper.NextReportID.Next(f.ctx)
	require.NoError(t, err)
	if reportID == 0 {
		reportID, err = f.keeper.NextReportID.Next(f.ctx)
		require.NoError(t, err)
	}
	height := f.sdkCtx().BlockHeight()
	r := types.Report{
		ReportId:        reportID,
		OperatorAddress: testOperator1,
		ServiceType:     testServiceType,
		Reporter:        testReporter,
		Reason:          "seed",
		FiledAt:         height,
		Status:          types.ReportStatus_REPORT_STATUS_PENDING,
		SlashAmount:     sdk.NewCoin(types.BondDenom, math.ZeroInt()),
		Deposit:         sdk.NewCoin(types.BondDenom, math.NewInt(10_000_000)),
	}
	require.NoError(t, f.keeper.Reports.Set(f.ctx, reportID, r))
	require.NoError(t, f.keeper.ReportsByOperator.Set(f.ctx, collections.Join3(testOperator1Addr.Bytes(), testServiceType, reportID)))
	require.NoError(t, f.keeper.PendingReportsQueue.Set(f.ctx, collections.Join(height, reportID)))
	return reportID
}

func TestMsgResolveReport_T1Dismiss(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	reportID := f.seedPendingReport(t)

	_, err := f.msgServer.ResolveReport(f.ctx, &types.MsgResolveReport{
		Controller: testController,
		ReportId:   reportID,
		Verdict:    types.ResolveVerdict_RESOLVE_VERDICT_T1_DISMISS,
	})
	require.NoError(t, err)

	r, err := f.keeper.Reports.Get(f.ctx, reportID)
	require.NoError(t, err)
	require.Equal(t, types.ReportStatus_REPORT_STATUS_RESOLVED_T1, r.Status)
	require.True(t, r.SlashAmount.Amount.IsZero())

	// Reporter deposit was refunded.
	require.Len(t, f.bankKeeper.ModToAcctCalls, 1)
	require.Equal(t, testReporterAddr, f.bankKeeper.ModToAcctCalls[0].Recipient)

	// Pending-queue entry removed.
	has, _ := f.keeper.PendingReportsQueue.Has(f.ctx, collections.Join(r.FiledAt, reportID))
	require.False(t, has)
}

func TestMsgResolveReport_T1DismissWithSlashBpsRejected(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	reportID := f.seedPendingReport(t)

	_, err := f.msgServer.ResolveReport(f.ctx, &types.MsgResolveReport{
		Controller: testController,
		ReportId:   reportID,
		Verdict:    types.ResolveVerdict_RESOLVE_VERDICT_T1_DISMISS,
		SlashBps:   100,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrInvalidVerdict.Error())
}

func TestMsgResolveReport_T1Slash(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	op := f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	reportID := f.seedPendingReport(t)

	slashBps := uint32(200)
	_, err := f.msgServer.ResolveReport(f.ctx, &types.MsgResolveReport{
		Controller: testController,
		ReportId:   reportID,
		Verdict:    types.ResolveVerdict_RESOLVE_VERDICT_T1_SLASH,
		SlashBps:   slashBps,
	})
	require.NoError(t, err)

	r, err := f.keeper.Reports.Get(f.ctx, reportID)
	require.NoError(t, err)
	require.Equal(t, types.ReportStatus_REPORT_STATUS_RESOLVED_T1, r.Status)
	require.True(t, r.SlashAmount.Amount.IsPositive())
	require.Equal(t, slashBps, r.ProposedSlashBps)

	// Operator's bond decreased.
	post, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.True(t, ok)
	require.True(t, post.Bond.Amount.LT(op.Bond.Amount))

	// Tier1Escrow row written for this report.
	escrowID, found := findEscrowFor(t, f, reportID)
	require.True(t, found)
	_, err = f.keeper.Tier1Escrow.Get(f.ctx, escrowID)
	require.NoError(t, err)

	// Tier1LastSlash entry written.
	has, _ := f.keeper.Tier1LastSlash.Has(f.ctx, collections.Join3(testControllerAddr.Bytes(), testOperator1Addr.Bytes(), testServiceType))
	require.True(t, has)
	_ = cfg
}

func TestMsgResolveReport_T1SlashRequiresPositiveBps(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	reportID := f.seedPendingReport(t)

	_, err := f.msgServer.ResolveReport(f.ctx, &types.MsgResolveReport{
		Controller: testController,
		ReportId:   reportID,
		Verdict:    types.ResolveVerdict_RESOLVE_VERDICT_T1_SLASH,
		SlashBps:   0,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrInvalidVerdict.Error())
}

func TestMsgResolveReport_T1SlashAboveCap(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	reportID := f.seedPendingReport(t)

	_, err := f.msgServer.ResolveReport(f.ctx, &types.MsgResolveReport{
		Controller: testController,
		ReportId:   reportID,
		Verdict:    types.ResolveVerdict_RESOLVE_VERDICT_T1_SLASH,
		SlashBps:   cfg.UnilateralSlashCapBps + 1,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrSlashCapExceeded.Error())
}

func TestMsgResolveReport_Escalate(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	reportID := f.seedPendingReport(t)

	_, err := f.msgServer.ResolveReport(f.ctx, &types.MsgResolveReport{
		Controller: testController,
		ReportId:   reportID,
		Verdict:    types.ResolveVerdict_RESOLVE_VERDICT_ESCALATE_TO_JURY,
		SlashBps:   300,
	})
	require.NoError(t, err)

	r, err := f.keeper.Reports.Get(f.ctx, reportID)
	require.NoError(t, err)
	require.Equal(t, types.ReportStatus_REPORT_STATUS_ESCALATED, r.Status)
	require.EqualValues(t, 300, r.ProposedSlashBps)
	require.NotZero(t, r.JuryCaseId, "jury case id assigned by rep keeper mock")

	// Pending queue cleared, escalated queue populated.
	hasP, _ := f.keeper.PendingReportsQueue.Has(f.ctx, collections.Join(r.FiledAt, reportID))
	require.False(t, hasP)
	hasE, _ := f.keeper.EscalatedReportsQueue.Has(f.ctx, collections.Join(r.EscalatedAt, reportID))
	require.True(t, hasE)
}

func TestMsgResolveReport_UnauthorizedController(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	reportID := f.seedPendingReport(t)

	_, err := f.msgServer.ResolveReport(f.ctx, &types.MsgResolveReport{
		Controller: testRandom,
		ReportId:   reportID,
		Verdict:    types.ResolveVerdict_RESOLVE_VERDICT_T1_DISMISS,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrUnauthorizedController.Error())
}

func TestMsgResolveReport_ReportNotFound(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)

	_, err := f.msgServer.ResolveReport(f.ctx, &types.MsgResolveReport{
		Controller: testController,
		ReportId:   999,
		Verdict:    types.ResolveVerdict_RESOLVE_VERDICT_T1_DISMISS,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrReportNotFound.Error())
}

func TestMsgResolveReport_AlreadyResolved(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	reportID := f.seedPendingReport(t)

	// Resolve once.
	_, err := f.msgServer.ResolveReport(f.ctx, &types.MsgResolveReport{
		Controller: testController,
		ReportId:   reportID,
		Verdict:    types.ResolveVerdict_RESOLVE_VERDICT_T1_DISMISS,
	})
	require.NoError(t, err)

	// Second resolve must fail.
	_, err = f.msgServer.ResolveReport(f.ctx, &types.MsgResolveReport{
		Controller: testController,
		ReportId:   reportID,
		Verdict:    types.ResolveVerdict_RESOLVE_VERDICT_T1_DISMISS,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrReportAlreadyResolved.Error())
}

// findEscrowFor walks Tier1Escrow and returns the (id, ok) for the row
// whose report_id matches.
func findEscrowFor(t *testing.T, f *fixture, reportID uint64) (uint64, bool) {
	t.Helper()
	var found uint64
	var ok bool
	err := f.keeper.Tier1Escrow.Walk(f.ctx, nil, func(id uint64, e types.Tier1EscrowEntry) (bool, error) {
		if e.ReportId == reportID {
			found = id
			ok = true
			return true, nil
		}
		return false, nil
	})
	require.NoError(t, err)
	return found, ok
}
