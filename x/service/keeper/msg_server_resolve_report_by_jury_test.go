package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

// seedEscalatedReport drives a fresh PENDING report through ESCALATE_TO_JURY,
// returning the report id with op still ACTIVE and JuryCaseId set.
func (f *fixture) seedEscalatedReport(t *testing.T, proposedBps uint32) uint64 {
	t.Helper()
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	reportID := f.seedPendingReport(t)
	_, err := f.msgServer.ResolveReport(f.ctx, &types.MsgResolveReport{
		Controller: testController,
		ReportId:   reportID,
		Verdict:    types.ResolveVerdict_RESOLVE_VERDICT_ESCALATE_TO_JURY,
		SlashBps:   proposedBps,
	})
	require.NoError(t, err)
	return reportID
}

func TestMsgResolveReportByJury_Accept(t *testing.T) {
	f := initFixture(t)
	reportID := f.seedEscalatedReport(t, 300)

	preBond := math.ZeroInt()
	if op, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType); ok {
		preBond = op.Bond.Amount
	}

	_, err := f.msgServer.ResolveReportByJury(f.ctx, &types.MsgResolveReportByJury{
		JuryAuthority: testCouncil,
		ReportId:      reportID,
		Verdict:       types.JuryVerdict_JURY_VERDICT_ACCEPT,
		SlashBps:      300,
	})
	require.NoError(t, err)

	r, err := f.keeper.Reports.Get(f.ctx, reportID)
	require.NoError(t, err)
	require.Equal(t, types.ReportStatus_REPORT_STATUS_RESOLVED_T2, r.Status)
	require.True(t, r.SlashAmount.Amount.IsPositive())

	// Operator's bond decreased.
	op, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.True(t, ok)
	require.True(t, op.Bond.Amount.LT(preBond))

	// Slash routed to community pool.
	require.NotEmpty(t, f.distributionKeeper.Calls)

	// Reporter deposit refunded.
	require.Len(t, f.bankKeeper.ModToAcctCalls, 1)
	require.Equal(t, testReporterAddr, f.bankKeeper.ModToAcctCalls[0].Recipient)
}

func TestMsgResolveReportByJury_AcceptDissolve(t *testing.T) {
	f := initFixture(t)
	reportID := f.seedEscalatedReport(t, 300)

	_, err := f.msgServer.ResolveReportByJury(f.ctx, &types.MsgResolveReportByJury{
		JuryAuthority: testCouncil,
		ReportId:      reportID,
		Verdict:       types.JuryVerdict_JURY_VERDICT_ACCEPT,
		SlashBps:      300,
		Dissolve:      true,
	})
	require.NoError(t, err)

	// Live record gone (operator dissolved).
	_, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.False(t, ok)

	// Dissolution hook fired.
	require.Len(t, f.hooks.Dissolved, 1)
	require.Equal(t, testOperator1Addr, f.hooks.Dissolved[0].Operator)
}

func TestMsgResolveReportByJury_Reduce(t *testing.T) {
	f := initFixture(t)
	reportID := f.seedEscalatedReport(t, 400)

	_, err := f.msgServer.ResolveReportByJury(f.ctx, &types.MsgResolveReportByJury{
		JuryAuthority: testCouncil,
		ReportId:      reportID,
		Verdict:       types.JuryVerdict_JURY_VERDICT_REDUCE,
		SlashBps:      200,
	})
	require.NoError(t, err)

	r, err := f.keeper.Reports.Get(f.ctx, reportID)
	require.NoError(t, err)
	require.Equal(t, types.ReportStatus_REPORT_STATUS_RESOLVED_T2, r.Status)
}

func TestMsgResolveReportByJury_ReduceRequiresSmallerBps(t *testing.T) {
	f := initFixture(t)
	reportID := f.seedEscalatedReport(t, 400)

	_, err := f.msgServer.ResolveReportByJury(f.ctx, &types.MsgResolveReportByJury{
		JuryAuthority: testCouncil,
		ReportId:      reportID,
		Verdict:       types.JuryVerdict_JURY_VERDICT_REDUCE,
		SlashBps:      400,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrInvalidVerdict.Error())
}

func TestMsgResolveReportByJury_Reject(t *testing.T) {
	f := initFixture(t)
	reportID := f.seedEscalatedReport(t, 300)

	// Jury actually voted REJECT — verdict cross-check expects that.
	f.repKeeper.GetJuryVerdictNameFn = func(_ context.Context, _ uint64) (string, error) {
		return types.JuryVerdictRejectChallenge, nil
	}

	_, err := f.msgServer.ResolveReportByJury(f.ctx, &types.MsgResolveReportByJury{
		JuryAuthority: testCouncil,
		ReportId:      reportID,
		Verdict:       types.JuryVerdict_JURY_VERDICT_REJECT,
	})
	require.NoError(t, err)

	r, err := f.keeper.Reports.Get(f.ctx, reportID)
	require.NoError(t, err)
	require.Equal(t, types.ReportStatus_REPORT_STATUS_RESOLVED_T2, r.Status)
	require.True(t, r.SlashAmount.Amount.IsZero())

	// Reporter deposit forfeited to community pool.
	require.NotEmpty(t, f.distributionKeeper.Calls)
}

func TestMsgResolveReportByJury_UnauthorizedResolver(t *testing.T) {
	f := initFixture(t)
	reportID := f.seedEscalatedReport(t, 300)

	_, err := f.msgServer.ResolveReportByJury(f.ctx, &types.MsgResolveReportByJury{
		JuryAuthority: testRandom, // not council
		ReportId:      reportID,
		Verdict:       types.JuryVerdict_JURY_VERDICT_ACCEPT,
		SlashBps:      300,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrUnauthorizedCouncilResolver.Error())
}

func TestMsgResolveReportByJury_VerdictMismatch(t *testing.T) {
	f := initFixture(t)
	reportID := f.seedEscalatedReport(t, 300)

	// Jury actually voted REJECT; resolver submits ACCEPT.
	f.repKeeper.GetJuryVerdictNameFn = func(_ context.Context, _ uint64) (string, error) {
		return types.JuryVerdictRejectChallenge, nil
	}

	_, err := f.msgServer.ResolveReportByJury(f.ctx, &types.MsgResolveReportByJury{
		JuryAuthority: testCouncil,
		ReportId:      reportID,
		Verdict:       types.JuryVerdict_JURY_VERDICT_ACCEPT,
		SlashBps:      300,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrJuryVerdictMismatch.Error())
}

func TestMsgResolveReportByJury_AcceptRequiresMatchingBps(t *testing.T) {
	f := initFixture(t)
	reportID := f.seedEscalatedReport(t, 300)

	_, err := f.msgServer.ResolveReportByJury(f.ctx, &types.MsgResolveReportByJury{
		JuryAuthority: testCouncil,
		ReportId:      reportID,
		Verdict:       types.JuryVerdict_JURY_VERDICT_ACCEPT,
		SlashBps:      200, // proposed was 300
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrInvalidVerdict.Error())
}

func TestMsgResolveReportByJury_ReportNotEscalated(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	reportID := f.seedPendingReport(t) // still PENDING

	_, err := f.msgServer.ResolveReportByJury(f.ctx, &types.MsgResolveReportByJury{
		JuryAuthority: testCouncil,
		ReportId:      reportID,
		Verdict:       types.JuryVerdict_JURY_VERDICT_REJECT,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrInvalidVerdict.Error())
}

func TestMsgResolveReportByJury_EscalatedQueueCleared(t *testing.T) {
	f := initFixture(t)
	reportID := f.seedEscalatedReport(t, 300)

	r, err := f.keeper.Reports.Get(f.ctx, reportID)
	require.NoError(t, err)
	escalatedAt := r.EscalatedAt

	_, err = f.msgServer.ResolveReportByJury(f.ctx, &types.MsgResolveReportByJury{
		JuryAuthority: testCouncil,
		ReportId:      reportID,
		Verdict:       types.JuryVerdict_JURY_VERDICT_ACCEPT,
		SlashBps:      300,
	})
	require.NoError(t, err)

	has, _ := f.keeper.EscalatedReportsQueue.Has(f.ctx, collections.Join(escalatedAt, reportID))
	require.False(t, has)
}
