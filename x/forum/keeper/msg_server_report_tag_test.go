package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestMsgServerReportTag(t *testing.T) {
	f := initFixture(t)

	t.Run("invalid creator address", func(t *testing.T) {
		msg := &types.MsgReportTag{
			Creator: "invalid",
			TagName: "test-tag",
			Reason:  "spam",
		}
		_, err := f.msgServer.ReportTag(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("tag not found", func(t *testing.T) {
		msg := &types.MsgReportTag{
			Creator: testCreator,
			TagName: "nonexistent-tag",
			Reason:  "spam",
		}
		_, err := f.msgServer.ReportTag(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTagNotFound)
	})

	t.Run("successful report", func(t *testing.T) {
		f.createTestTag(t, "report-test-tag")

		msg := &types.MsgReportTag{
			Creator: testCreator,
			TagName: "report-test-tag",
			Reason:  "inappropriate content",
		}
		_, err := f.msgServer.ReportTag(f.ctx, msg)
		require.NoError(t, err)

		// Verify report was created
		report, err := f.keeper.TagReport.Get(f.ctx, "report-test-tag")
		require.NoError(t, err)
		require.Equal(t, "report-test-tag", report.TagName)
		require.Contains(t, report.Reporters, testCreator)
	})

	t.Run("already reported by same user", func(t *testing.T) {
		f.createTestTag(t, "double-report-tag")

		// Create existing report
		report := types.TagReport{
			TagName:       "double-report-tag",
			TotalBond:     "100",
			FirstReportAt: f.sdkCtx().BlockTime().Unix(),
			Reporters:     []string{testCreator},
		}
		f.keeper.TagReport.Set(f.ctx, "double-report-tag", report)

		msg := &types.MsgReportTag{
			Creator: testCreator,
			TagName: "double-report-tag",
			Reason:  "spam",
		}
		_, err := f.msgServer.ReportTag(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrReportAlreadyExists)
	})

	t.Run("add additional reporter", func(t *testing.T) {
		f.createTestTag(t, "multi-report-tag")

		// Create existing report from different user
		report := types.TagReport{
			TagName:       "multi-report-tag",
			TotalBond:     "100",
			FirstReportAt: f.sdkCtx().BlockTime().Unix(),
			Reporters:     []string{testCreator},
		}
		f.keeper.TagReport.Set(f.ctx, "multi-report-tag", report)

		msg := &types.MsgReportTag{
			Creator: testCreator2,
			TagName: "multi-report-tag",
			Reason:  "also found it problematic",
		}
		_, err := f.msgServer.ReportTag(f.ctx, msg)
		require.NoError(t, err)

		// Verify reporter was added
		updatedReport, err := f.keeper.TagReport.Get(f.ctx, "multi-report-tag")
		require.NoError(t, err)
		require.Contains(t, updatedReport.Reporters, testCreator)
		require.Contains(t, updatedReport.Reporters, testCreator2)
	})
}
