package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryMemberStanding(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.MemberStanding(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty member address", func(t *testing.T) {
		_, err := qs.MemberStanding(f.ctx, &types.QueryMemberStandingRequest{Member: ""})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("member with no warnings or reports", func(t *testing.T) {
		resp, err := qs.MemberStanding(f.ctx, &types.QueryMemberStandingRequest{Member: testCreator})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.WarningCount)
		require.False(t, resp.ActiveReport)
		require.Equal(t, uint64(5), resp.TrustTier) // Stub returns 5
	})

	t.Run("member with warnings", func(t *testing.T) {
		// Create warnings for this member
		warning1 := types.MemberWarning{
			Id:       1,
			Member:   testCreator2,
			Reason:   "First warning",
			IssuedAt: f.sdkCtx().BlockTime().Unix(),
		}
		warning2 := types.MemberWarning{
			Id:       2,
			Member:   testCreator2,
			Reason:   "Second warning",
			IssuedAt: f.sdkCtx().BlockTime().Unix(),
		}
		f.keeper.MemberWarning.Set(f.ctx, 1, warning1)
		f.keeper.MemberWarning.Set(f.ctx, 2, warning2)

		resp, err := qs.MemberStanding(f.ctx, &types.QueryMemberStandingRequest{Member: testCreator2})
		require.NoError(t, err)
		require.Equal(t, uint64(2), resp.WarningCount)
	})

	t.Run("member with active report", func(t *testing.T) {
		// Create active report for this member
		report := types.MemberReport{
			Member:    testSentinel,
			Reason:    "spam",
			Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
			CreatedAt: f.sdkCtx().BlockTime().Unix(),
		}
		f.keeper.MemberReport.Set(f.ctx, testSentinel, report)

		resp, err := qs.MemberStanding(f.ctx, &types.QueryMemberStandingRequest{Member: testSentinel})
		require.NoError(t, err)
		require.True(t, resp.ActiveReport)
	})
}
