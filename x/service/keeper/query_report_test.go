package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/service/types"
)

func TestQueryReport(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	reportID := f.seedPendingReport(t)

	resp, err := f.queryServer.Report(f.ctx, &types.QueryReportRequest{ReportId: reportID})
	require.NoError(t, err)
	require.Equal(t, reportID, resp.Report.ReportId)
	require.Equal(t, types.ReportStatus_REPORT_STATUS_PENDING, resp.Report.Status)
}

func TestQueryReport_NotFound(t *testing.T) {
	f := initFixture(t)

	_, err := f.queryServer.Report(f.ctx, &types.QueryReportRequest{ReportId: 9999})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestQueryReport_NilRequest(t *testing.T) {
	f := initFixture(t)
	_, err := f.queryServer.Report(f.ctx, nil)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
