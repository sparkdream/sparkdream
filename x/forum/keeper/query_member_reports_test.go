package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryMemberReports(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.MemberReports(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no reports", func(t *testing.T) {
		resp, err := qs.MemberReports(f.ctx, &types.QueryMemberReportsRequest{})
		require.NoError(t, err)
		require.Empty(t, resp.Member)
	})

	t.Run("has reports", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()

		// Create member report
		report := types.MemberReport{
			Member:    testCreator,
			Reason:    "spam",
			Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
			CreatedAt: now,
			Reporters: []string{testSentinel},
		}
		f.keeper.MemberReport.Set(f.ctx, testCreator, report)

		resp, err := qs.MemberReports(f.ctx, &types.QueryMemberReportsRequest{})
		require.NoError(t, err)
		require.Equal(t, testCreator, resp.Member)
	})

	t.Run("multiple reports", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()

		// Create another report
		report := types.MemberReport{
			Member:    testCreator2,
			Reason:    "harassment",
			Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
			CreatedAt: now,
			Reporters: []string{testSentinel},
		}
		f.keeper.MemberReport.Set(f.ctx, testCreator2, report)

		resp, err := qs.MemberReports(f.ctx, &types.QueryMemberReportsRequest{})
		require.NoError(t, err)
		// Should have multiple reports now
		require.NotEmpty(t, resp.Member)
	})
}
