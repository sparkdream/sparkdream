package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestMsgDefendMemberReport(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	t.Run("invalid creator address", func(t *testing.T) {
		_, err := ms.DefendMemberReport(f.ctx, &types.MsgDefendMemberReport{
			Creator: "invalid",
			Defense: "I am innocent",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("no report for creator", func(t *testing.T) {
		_, err := ms.DefendMemberReport(f.ctx, &types.MsgDefendMemberReport{
			Creator: testCreator,
			Defense: "I am innocent",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "no report found")
	})

	t.Run("report not in defendable state", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()

		// Create resolved report
		report := types.MemberReport{
			Member:    testCreator,
			Reason:    "spam",
			Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_RESOLVED,
			CreatedAt: now,
			Reporters: []string{testSentinel},
		}
		f.keeper.MemberReport.Set(f.ctx, testCreator, report)

		_, err := ms.DefendMemberReport(f.ctx, &types.MsgDefendMemberReport{
			Creator: testCreator,
			Defense: "I am innocent",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not in a defendable state")
	})

	t.Run("defense already submitted", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()

		// Create pending report with defense already set
		report := types.MemberReport{
			Member:             testCreator2,
			Reason:             "spam",
			Status:             types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
			CreatedAt:          now,
			Reporters:          []string{testSentinel},
			Defense:            "Already defended",
			DefenseSubmittedAt: now,
		}
		f.keeper.MemberReport.Set(f.ctx, testCreator2, report)

		_, err := ms.DefendMemberReport(f.ctx, &types.MsgDefendMemberReport{
			Creator: testCreator2,
			Defense: "Another defense",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "defense already submitted")
	})

	t.Run("success pending", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()

		// Create pending report
		report := types.MemberReport{
			Member:    testSentinel,
			Reason:    "harassment",
			Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
			CreatedAt: now,
			Reporters: []string{testCreator},
		}
		f.keeper.MemberReport.Set(f.ctx, testSentinel, report)

		_, err := ms.DefendMemberReport(f.ctx, &types.MsgDefendMemberReport{
			Creator: testSentinel,
			Defense: "I was misunderstood",
		})
		require.NoError(t, err)

		// Verify defense was added
		updated, err := f.keeper.MemberReport.Get(f.ctx, testSentinel)
		require.NoError(t, err)
		require.Equal(t, "I was misunderstood", updated.Defense)
		require.NotZero(t, updated.DefenseSubmittedAt)
	})

	t.Run("success escalated", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()

		// Create escalated report
		report := types.MemberReport{
			Member:    testCreator,
			Reason:    "fraud",
			Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_ESCALATED,
			CreatedAt: now,
			Reporters: []string{testCreator2, testSentinel},
		}
		f.keeper.MemberReport.Set(f.ctx, testCreator, report)

		_, err := ms.DefendMemberReport(f.ctx, &types.MsgDefendMemberReport{
			Creator: testCreator,
			Defense: "These accusations are false",
		})
		require.NoError(t, err)

		// Verify defense was added
		updated, err := f.keeper.MemberReport.Get(f.ctx, testCreator)
		require.NoError(t, err)
		require.Equal(t, "These accusations are false", updated.Defense)
	})
}
