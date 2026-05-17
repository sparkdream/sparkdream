package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

func TestQueryReportsByOperator(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	f.seedPendingReport(t)
	f.seedPendingReport(t)

	resp, err := f.queryServer.ReportsByOperator(f.ctx, &types.QueryReportsByOperatorRequest{
		OperatorAddress: testOperator1,
		ServiceType:     testServiceType,
	})
	require.NoError(t, err)
	require.Len(t, resp.Reports, 2)
}

func TestQueryReportsByOperator_StatusFilter(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	r1 := f.seedPendingReport(t)
	f.seedPendingReport(t)

	// Flip r1 to RESOLVED_T1.
	rec, _ := f.keeper.Reports.Get(f.ctx, r1)
	rec.Status = types.ReportStatus_REPORT_STATUS_RESOLVED_T1
	require.NoError(t, f.keeper.Reports.Set(f.ctx, r1, rec))

	respP, err := f.queryServer.ReportsByOperator(f.ctx, &types.QueryReportsByOperatorRequest{
		OperatorAddress: testOperator1,
		ServiceType:     testServiceType,
		StatusFilter:    "PENDING",
	})
	require.NoError(t, err)
	require.Len(t, respP.Reports, 1)
	require.Equal(t, types.ReportStatus_REPORT_STATUS_PENDING, respP.Reports[0].Status)

	respR, err := f.queryServer.ReportsByOperator(f.ctx, &types.QueryReportsByOperatorRequest{
		OperatorAddress: testOperator1,
		ServiceType:     testServiceType,
		StatusFilter:    "RESOLVED_T1",
	})
	require.NoError(t, err)
	require.Len(t, respR.Reports, 1)
	require.Equal(t, types.ReportStatus_REPORT_STATUS_RESOLVED_T1, respR.Reports[0].Status)
}

func TestQueryReportsByOperator_EmptyAddressRejected(t *testing.T) {
	f := initFixture(t)
	_, err := f.queryServer.ReportsByOperator(f.ctx, &types.QueryReportsByOperatorRequest{
		OperatorAddress: "",
		ServiceType:     testServiceType,
	})
	require.Error(t, err)
}

func TestQueryReportsByOperator_BadStatusFilterRejected(t *testing.T) {
	f := initFixture(t)
	_, err := f.queryServer.ReportsByOperator(f.ctx, &types.QueryReportsByOperatorRequest{
		OperatorAddress: testOperator1,
		ServiceType:     testServiceType,
		StatusFilter:    "not-a-real-status",
	})
	require.Error(t, err)
}
