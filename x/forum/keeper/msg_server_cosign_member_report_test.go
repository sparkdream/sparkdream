package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestMsgCosignMemberReport(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	t.Run("invalid creator address", func(t *testing.T) {
		_, err := ms.CosignMemberReport(f.ctx, &types.MsgCosignMemberReport{
			Creator: "invalid",
			Member:  testCreator2,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("report not found", func(t *testing.T) {
		_, err := ms.CosignMemberReport(f.ctx, &types.MsgCosignMemberReport{
			Creator: testCreator,
			Member:  testCreator2,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "no report found")
	})

	t.Run("report not pending", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()

		// Create resolved report
		report := types.MemberReport{
			Member:    testCreator2,
			Reason:    "spam",
			Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_RESOLVED,
			CreatedAt: now,
			Reporters: []string{testSentinel},
			TotalBond: "1000",
		}
		f.keeper.MemberReport.Set(f.ctx, testCreator2, report)

		_, err := ms.CosignMemberReport(f.ctx, &types.MsgCosignMemberReport{
			Creator: testCreator,
			Member:  testCreator2,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not pending")
	})

	t.Run("already cosigned", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()

		// Create pending report where creator already reported
		report := types.MemberReport{
			Member:    testSentinel,
			Reason:    "spam",
			Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
			CreatedAt: now,
			Reporters: []string{testCreator}, // Creator already in reporters
			TotalBond: "1000",
		}
		f.keeper.MemberReport.Set(f.ctx, testSentinel, report)

		_, err := ms.CosignMemberReport(f.ctx, &types.MsgCosignMemberReport{
			Creator: testCreator,
			Member:  testSentinel,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "already co-signed")
	})

	t.Run("success", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()

		// Create pending report
		report := types.MemberReport{
			Member:    testCreator,
			Reason:    "harassment",
			Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
			CreatedAt: now,
			Reporters: []string{testSentinel},
			TotalBond: "1000",
		}
		f.keeper.MemberReport.Set(f.ctx, testCreator, report)

		// Cosign with different sentinel
		_, err := ms.CosignMemberReport(f.ctx, &types.MsgCosignMemberReport{
			Creator: testCreator2, // Different from original reporter
			Member:  testCreator,
		})
		require.NoError(t, err)

		// Verify cosigner added
		updated, err := f.keeper.MemberReport.Get(f.ctx, testCreator)
		require.NoError(t, err)
		require.Len(t, updated.Reporters, 2)
		require.Contains(t, updated.Reporters, testCreator2)
	})
}
