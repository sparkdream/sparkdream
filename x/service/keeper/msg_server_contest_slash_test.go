package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

// seedResolvedT1ReportAndSlash drives a fresh PENDING report through a
// T1_SLASH resolution, returning the report id. The operator is left
// with the slash applied and a live Tier1Escrow entry.
func (f *fixture) seedResolvedT1ReportAndSlash(t *testing.T, slashBps uint32) uint64 {
	t.Helper()
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	reportID := f.seedPendingReport(t)
	_, err := f.msgServer.ResolveReport(f.ctx, &types.MsgResolveReport{
		Controller: testController,
		ReportId:   reportID,
		Verdict:    types.ResolveVerdict_RESOLVE_VERDICT_T1_SLASH,
		SlashBps:   slashBps,
	})
	require.NoError(t, err)
	return reportID
}

func TestMsgContestSlash_HappyPath(t *testing.T) {
	f := initFixture(t)
	reportID := f.seedResolvedT1ReportAndSlash(t, 200)

	preBond := math.ZeroInt()
	if op, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType); ok {
		preBond = op.BondAmount
	}

	_, err := f.msgServer.ContestSlash(f.ctx, &types.MsgContestSlash{
		Operator:    testOperator1,
		ServiceType: testServiceType,
		ReportId:    reportID,
	})
	require.NoError(t, err)

	r, err := f.keeper.Reports.Get(f.ctx, reportID)
	require.NoError(t, err)
	require.Equal(t, types.ReportStatus_REPORT_STATUS_ESCALATED, r.Status)

	// Bond is restored — must exceed pre-contest amount.
	op, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.True(t, ok)
	require.True(t, op.BondAmount.GT(preBond))

	// Escalated queue populated.
	hasE, _ := f.keeper.EscalatedReportsQueue.Has(f.ctx, collections.Join(r.EscalatedAt, reportID))
	require.True(t, hasE)
}

func TestMsgContestSlash_ReportNotFound(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))

	_, err := f.msgServer.ContestSlash(f.ctx, &types.MsgContestSlash{
		Operator:    testOperator1,
		ServiceType: testServiceType,
		ReportId:    999,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrReportNotFound.Error())
}

func TestMsgContestSlash_NotOperator(t *testing.T) {
	f := initFixture(t)
	reportID := f.seedResolvedT1ReportAndSlash(t, 200)

	_, err := f.msgServer.ContestSlash(f.ctx, &types.MsgContestSlash{
		Operator:    testOperator2,
		ServiceType: testServiceType,
		ReportId:    reportID,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrUnauthorizedController.Error())
}

func TestMsgContestSlash_ServiceTypeMismatch(t *testing.T) {
	f := initFixture(t)
	reportID := f.seedResolvedT1ReportAndSlash(t, 200)

	_, err := f.msgServer.ContestSlash(f.ctx, &types.MsgContestSlash{
		Operator:    testOperator1,
		ServiceType: "wrong",
		ReportId:    reportID,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrInvalidVerdict.Error())
}

func TestMsgContestSlash_NotResolvedT1(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	reportID := f.seedPendingReport(t) // still PENDING

	_, err := f.msgServer.ContestSlash(f.ctx, &types.MsgContestSlash{
		Operator:    testOperator1,
		ServiceType: testServiceType,
		ReportId:    reportID,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrInvalidVerdict.Error())
}

func TestMsgContestSlash_WindowExpired(t *testing.T) {
	f := initFixture(t)
	reportID := f.seedResolvedT1ReportAndSlash(t, 200)

	// Advance past the contest window.
	params, _ := f.keeper.Params.Get(f.ctx)
	f.withBlockHeight(f.sdkCtx().BlockHeight() + params.ReportContestWindowBlocks + 1)

	_, err := f.msgServer.ContestSlash(f.ctx, &types.MsgContestSlash{
		Operator:    testOperator1,
		ServiceType: testServiceType,
		ReportId:    reportID,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrContestWindowExpired.Error())
}
