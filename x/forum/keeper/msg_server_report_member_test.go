package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestMsgServerReportMember(t *testing.T) {
	f := initFixture(t)

	t.Run("invalid creator address", func(t *testing.T) {
		msg := &types.MsgReportMember{
			Creator: "invalid",
			Member:  testCreator,
			Reason:  "spam",
		}
		_, err := f.msgServer.ReportMember(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("invalid member address", func(t *testing.T) {
		msg := &types.MsgReportMember{
			Creator: testCreator,
			Member:  "invalid",
			Reason:  "spam",
		}
		_, err := f.msgServer.ReportMember(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid member address")
	})

	t.Run("cannot report self", func(t *testing.T) {
		msg := &types.MsgReportMember{
			Creator: testCreator,
			Member:  testCreator,
			Reason:  "spam",
		}
		_, err := f.msgServer.ReportMember(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrCannotReportSelf)
	})

	t.Run("successful report", func(t *testing.T) {
		// With stubs, all users have high tier and bond
		msg := &types.MsgReportMember{
			Creator: testSentinel,
			Member:  testCreator2,
			Reason:  "repeated spam",
		}
		_, err := f.msgServer.ReportMember(f.ctx, msg)
		require.NoError(t, err)

		// Verify report was created
		report, err := f.keeper.MemberReport.Get(f.ctx, testCreator2)
		require.NoError(t, err)
		require.Equal(t, testCreator2, report.Member)
		require.Equal(t, "repeated spam", report.Reason)
		require.Contains(t, report.Reporters, testSentinel)
		require.Equal(t, types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING, report.Status)
	})

	t.Run("report already exists", func(t *testing.T) {
		// Create existing report
		report := types.MemberReport{
			Member:    testCreator,
			Reason:    "existing report",
			CreatedAt: f.sdkCtx().BlockTime().Unix(),
			Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
			Reporters: []string{testCreator2},
		}
		f.keeper.MemberReport.Set(f.ctx, testCreator, report)

		msg := &types.MsgReportMember{
			Creator: testSentinel,
			Member:  testCreator,
			Reason:  "another report",
		}
		_, err := f.msgServer.ReportMember(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrReportAlreadyExists)
	})
}
