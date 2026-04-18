package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestQueryMemberReports_EmptyStore(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	resp, err := qs.MemberReports(f.ctx, &types.QueryMemberReportsRequest{})
	require.NoError(t, err)
	require.Empty(t, resp.Member)
	require.Zero(t, resp.Status)
}

func TestQueryMemberReports_ReturnsFirstReport(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	report := types.MemberReport{
		Member: "rep1",
		Status: types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
	}
	require.NoError(t, f.keeper.MemberReport.Set(f.ctx, "key1", report))

	resp, err := qs.MemberReports(f.ctx, &types.QueryMemberReportsRequest{})
	require.NoError(t, err)
	require.Equal(t, "rep1", resp.Member)
	require.Equal(t, uint64(types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING), resp.Status)
}

func TestQueryMemberReports_NilRequest(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.MemberReports(f.ctx, nil)
	require.Error(t, err)
}
