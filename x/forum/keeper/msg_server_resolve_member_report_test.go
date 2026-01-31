package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestMsgResolveMemberReport(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	t.Run("invalid creator address", func(t *testing.T) {
		_, err := ms.ResolveMemberReport(f.ctx, &types.MsgResolveMemberReport{
			Creator: "invalid",
			Member:  testCreator2,
			Action:  0,
			Reason:  "dismissed",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("not governance authority", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()

		// Create pending report
		report := types.MemberReport{
			Member:    testCreator2,
			Reason:    "spam",
			Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
			CreatedAt: now,
			Reporters: []string{testSentinel},
			TotalBond: "1000",
		}
		f.keeper.MemberReport.Set(f.ctx, testCreator2, report)

		// testCreator is not gov authority
		_, err := ms.ResolveMemberReport(f.ctx, &types.MsgResolveMemberReport{
			Creator: testCreator,
			Member:  testCreator2,
			Action:  0,
			Reason:  "dismissed",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "governance authority")
	})

	t.Run("report not found", func(t *testing.T) {
		// Use module authority
		authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

		_, err := ms.ResolveMemberReport(f.ctx, &types.MsgResolveMemberReport{
			Creator: authority,
			Member:  "nonexistent",
			Action:  0,
			Reason:  "dismissed",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "no report found")
	})

	t.Run("report already resolved", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()
		authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

		// Create resolved report
		report := types.MemberReport{
			Member:    testCreator,
			Reason:    "spam",
			Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_RESOLVED,
			CreatedAt: now,
			Reporters: []string{testSentinel},
			TotalBond: "1000",
		}
		f.keeper.MemberReport.Set(f.ctx, testCreator, report)

		_, err := ms.ResolveMemberReport(f.ctx, &types.MsgResolveMemberReport{
			Creator: authority,
			Member:  testCreator,
			Action:  0,
			Reason:  "dismissed",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "already resolved")
	})

	t.Run("success dismiss", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()
		authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

		// Create pending report
		report := types.MemberReport{
			Member:    testSentinel,
			Reason:    "spam",
			Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
			CreatedAt: now,
			Reporters: []string{testCreator},
			TotalBond: "1000",
		}
		f.keeper.MemberReport.Set(f.ctx, testSentinel, report)

		_, err := ms.ResolveMemberReport(f.ctx, &types.MsgResolveMemberReport{
			Creator: authority,
			Member:  testSentinel,
			Action:  0, // Dismiss
			Reason:  "no evidence of wrongdoing",
		})
		require.NoError(t, err)

		// Verify report was resolved
		updated, err := f.keeper.MemberReport.Get(f.ctx, testSentinel)
		require.NoError(t, err)
		require.Equal(t, types.MemberReportStatus_MEMBER_REPORT_STATUS_RESOLVED, updated.Status)
	})

	t.Run("success warning", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()
		authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

		// Create pending report
		report := types.MemberReport{
			Member:    testCreator2,
			Reason:    "harassment",
			Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
			CreatedAt: now,
			Reporters: []string{testCreator},
			TotalBond: "1000",
		}
		f.keeper.MemberReport.Set(f.ctx, testCreator2, report)

		_, err := ms.ResolveMemberReport(f.ctx, &types.MsgResolveMemberReport{
			Creator: authority,
			Member:  testCreator2,
			Action:  uint64(types.GovActionType_GOV_ACTION_TYPE_WARNING),
			Reason:  "first offense, issuing warning",
		})
		require.NoError(t, err)

		// Verify report was resolved
		updated, err := f.keeper.MemberReport.Get(f.ctx, testCreator2)
		require.NoError(t, err)
		require.Equal(t, types.MemberReportStatus_MEMBER_REPORT_STATUS_RESOLVED, updated.Status)

		// Verify warning was created
		var foundWarning bool
		warningIter, err := f.keeper.MemberWarning.Iterate(f.ctx, nil)
		require.NoError(t, err)
		defer warningIter.Close()
		for ; warningIter.Valid(); warningIter.Next() {
			warning, _ := warningIter.Value()
			if warning.Member == testCreator2 {
				foundWarning = true
				require.Equal(t, "first offense, issuing warning", warning.Reason)
				require.Equal(t, uint64(1), warning.WarningNumber)
				break
			}
		}
		require.True(t, foundWarning)
	})
}
