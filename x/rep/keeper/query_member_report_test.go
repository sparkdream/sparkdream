package keeper_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestQueryMemberReport(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Seed three reports.
	seed := []types.MemberReport{
		{Member: "member-phoenix", Status: types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING},
		{Member: "member-aurora", Status: types.MemberReportStatus_MEMBER_REPORT_STATUS_ESCALATED},
		{Member: "member-zenith", Status: types.MemberReportStatus_MEMBER_REPORT_STATUS_RESOLVED},
	}
	for _, r := range seed {
		require.NoError(t, f.keeper.MemberReport.Set(f.ctx, r.Member, r))
	}

	t.Run("get found", func(t *testing.T) {
		resp, err := qs.GetMemberReport(f.ctx, &types.QueryGetMemberReportRequest{Member: "member-phoenix"})
		require.NoError(t, err)
		require.Equal(t, "member-phoenix", resp.MemberReport.Member)
		require.Equal(t, types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING, resp.MemberReport.Status)
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := qs.GetMemberReport(f.ctx, &types.QueryGetMemberReportRequest{Member: "absent"})
		require.Error(t, err)
		require.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("get nil request", func(t *testing.T) {
		_, err := qs.GetMemberReport(f.ctx, nil)
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("list all", func(t *testing.T) {
		resp, err := qs.ListMemberReport(f.ctx, &types.QueryAllMemberReportRequest{
			Pagination: &query.PageRequest{CountTotal: true},
		})
		require.NoError(t, err)
		require.Len(t, resp.MemberReport, 3)
		require.Equal(t, uint64(3), resp.Pagination.Total)
	})

	t.Run("list paginated", func(t *testing.T) {
		resp, err := qs.ListMemberReport(f.ctx, &types.QueryAllMemberReportRequest{
			Pagination: &query.PageRequest{Limit: 1, CountTotal: true},
		})
		require.NoError(t, err)
		require.Len(t, resp.MemberReport, 1)
		require.NotEmpty(t, resp.Pagination.NextKey)
	})

	t.Run("list nil request", func(t *testing.T) {
		_, err := qs.ListMemberReport(f.ctx, nil)
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestQueryMemberReports(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.MemberReports(f.ctx, nil)
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("empty store returns empty response", func(t *testing.T) {
		resp, err := qs.MemberReports(f.ctx, &types.QueryMemberReportsRequest{})
		require.NoError(t, err)
		require.Equal(t, "", resp.Member)
	})

	t.Run("returns first report", func(t *testing.T) {
		require.NoError(t, f.keeper.MemberReport.Set(f.ctx, "member-first", types.MemberReport{
			Member: "member-first",
			Status: types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
		}))
		resp, err := qs.MemberReports(f.ctx, &types.QueryMemberReportsRequest{})
		require.NoError(t, err)
		require.Equal(t, "member-first", resp.Member)
		require.Equal(t, uint64(types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING), resp.Status)
	})
}
