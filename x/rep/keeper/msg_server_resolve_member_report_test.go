package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func authorizeCouncil(f *fixture) {
	f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, _ string, _ string, _ string) bool {
		return true
	}
}

func denyCouncil(f *fixture) {
	f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, _ string, _ string, _ string) bool {
		return false
	}
}

func TestMsgServerResolveMemberReport(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.ResolveMemberReport(f.ctx, &types.MsgResolveMemberReport{
			Creator: "invalid",
			Member:  "anything",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("not authorized", func(t *testing.T) {
		f := initFixture(t)
		denyCouncil(f)
		ms := keeper.NewMsgServerImpl(f.keeper)

		authority := sdk.AccAddress([]byte("unauthorized____"))
		authorityStr, err := f.addressCodec.BytesToString(authority)
		require.NoError(t, err)

		member := sdk.AccAddress([]byte("subject_notauth_"))
		memberStr, err := f.addressCodec.BytesToString(member)
		require.NoError(t, err)

		_, err = ms.ResolveMemberReport(f.ctx, &types.MsgResolveMemberReport{
			Creator: authorityStr,
			Member:  memberStr,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotGovAuthority)
	})

	t.Run("report not found", func(t *testing.T) {
		f := initFixture(t)
		authorizeCouncil(f)
		ms := keeper.NewMsgServerImpl(f.keeper)

		authority := sdk.AccAddress([]byte("auth_nofound____"))
		authorityStr, err := f.addressCodec.BytesToString(authority)
		require.NoError(t, err)

		member := sdk.AccAddress([]byte("subject_nofound_"))
		memberStr, err := f.addressCodec.BytesToString(member)
		require.NoError(t, err)

		_, err = ms.ResolveMemberReport(f.ctx, &types.MsgResolveMemberReport{
			Creator: authorityStr,
			Member:  memberStr,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrReportNotFound)
	})

	t.Run("report already resolved", func(t *testing.T) {
		f := initFixture(t)
		authorizeCouncil(f)
		ms := keeper.NewMsgServerImpl(f.keeper)

		authority := sdk.AccAddress([]byte("auth_resolved___"))
		authorityStr, err := f.addressCodec.BytesToString(authority)
		require.NoError(t, err)

		member := sdk.AccAddress([]byte("subj_resolved___"))
		memberStr, err := f.addressCodec.BytesToString(member)
		require.NoError(t, err)

		require.NoError(t, f.keeper.MemberReport.Set(f.ctx, memberStr, types.MemberReport{
			Member: memberStr,
			Status: types.MemberReportStatus_MEMBER_REPORT_STATUS_RESOLVED,
		}))

		_, err = ms.ResolveMemberReport(f.ctx, &types.MsgResolveMemberReport{
			Creator: authorityStr,
			Member:  memberStr,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrReportNotPending)
	})

	t.Run("dismiss refunds bonds", func(t *testing.T) {
		f := initFixture(t)
		authorizeCouncil(f)
		ms := keeper.NewMsgServerImpl(f.keeper)

		authority := sdk.AccAddress([]byte("auth_dismiss____"))
		authorityStr, err := f.addressCodec.BytesToString(authority)
		require.NoError(t, err)

		// Reporter with staked DREAM so refund path has something to unlock.
		reporter := sdk.AccAddress([]byte("reporter_dismiss"))
		require.NoError(t, f.keeper.Member.Set(f.ctx, reporter.String(), types.Member{
			Address:        reporter.String(),
			Status:         types.MemberStatus_MEMBER_STATUS_ACTIVE,
			DreamBalance:   keeper.PtrInt(math.NewInt(1000)),
			StakedDream:    keeper.PtrInt(math.NewInt(500)),
			LifetimeEarned: keeper.PtrInt(math.NewInt(1000)),
			LifetimeBurned: keeper.PtrInt(math.ZeroInt()),
			TrustLevel:     types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		}))
		reporterStr, err := f.addressCodec.BytesToString(reporter)
		require.NoError(t, err)

		member := sdk.AccAddress([]byte("subj_dismiss____"))
		memberStr, err := f.addressCodec.BytesToString(member)
		require.NoError(t, err)

		require.NoError(t, f.keeper.MemberReport.Set(f.ctx, memberStr, types.MemberReport{
			Member:    memberStr,
			Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
			Reporters: []string{reporterStr},
			TotalBond: "200",
		}))

		_, err = ms.ResolveMemberReport(f.ctx, &types.MsgResolveMemberReport{
			Creator: authorityStr,
			Member:  memberStr,
			Action:  uint64(types.GovActionType_GOV_ACTION_TYPE_UNSPECIFIED),
			Reason:  "dismissed",
		})
		require.NoError(t, err)

		report, err := f.keeper.MemberReport.Get(f.ctx, memberStr)
		require.NoError(t, err)
		require.Equal(t, types.MemberReportStatus_MEMBER_REPORT_STATUS_RESOLVED, report.Status)

		// Reporter's staked DREAM reduced (refunded back to unlocked balance).
		refundedReporter, err := f.keeper.Member.Get(f.ctx, reporter.String())
		require.NoError(t, err)
		require.Equal(t, int64(300), refundedReporter.StakedDream.Int64(), "staked DREAM should decrease by 200")
	})

	t.Run("warning creates MemberWarning record", func(t *testing.T) {
		f := initFixture(t)
		authorizeCouncil(f)
		ms := keeper.NewMsgServerImpl(f.keeper)

		authority := sdk.AccAddress([]byte("auth_warn_______"))
		authorityStr, err := f.addressCodec.BytesToString(authority)
		require.NoError(t, err)

		reporter := sdk.AccAddress([]byte("reporter_warn___"))
		require.NoError(t, f.keeper.Member.Set(f.ctx, reporter.String(), types.Member{
			Address:        reporter.String(),
			Status:         types.MemberStatus_MEMBER_STATUS_ACTIVE,
			DreamBalance:   keeper.PtrInt(math.NewInt(1000)),
			StakedDream:    keeper.PtrInt(math.NewInt(500)),
			LifetimeEarned: keeper.PtrInt(math.NewInt(1000)),
			LifetimeBurned: keeper.PtrInt(math.ZeroInt()),
			TrustLevel:     types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		}))
		reporterStr, err := f.addressCodec.BytesToString(reporter)
		require.NoError(t, err)

		member := sdk.AccAddress([]byte("subj_warn_______"))
		memberStr, err := f.addressCodec.BytesToString(member)
		require.NoError(t, err)

		require.NoError(t, f.keeper.MemberReport.Set(f.ctx, memberStr, types.MemberReport{
			Member:    memberStr,
			Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
			Reporters: []string{reporterStr},
			TotalBond: "100",
		}))

		_, err = ms.ResolveMemberReport(f.ctx, &types.MsgResolveMemberReport{
			Creator: authorityStr,
			Member:  memberStr,
			Action:  uint64(types.GovActionType_GOV_ACTION_TYPE_WARNING),
			Reason:  "first warning",
		})
		require.NoError(t, err)

		// A warning record exists for this member.
		foundWarning := false
		require.NoError(t, f.keeper.MemberWarning.Walk(f.ctx, nil, func(_ uint64, w types.MemberWarning) (bool, error) {
			if w.Member == memberStr && w.Reason == "first warning" {
				foundWarning = true
				require.Equal(t, uint64(1), w.WarningNumber)
			}
			return false, nil
		}))
		require.True(t, foundWarning, "expected a MemberWarning record to be created")
	})

	t.Run("zeroing calls ZeroMember", func(t *testing.T) {
		f := initFixture(t)
		authorizeCouncil(f)
		ms := keeper.NewMsgServerImpl(f.keeper)

		authority := sdk.AccAddress([]byte("auth_zero_______"))
		authorityStr, err := f.addressCodec.BytesToString(authority)
		require.NoError(t, err)

		// Reporter
		reporter := sdk.AccAddress([]byte("reporter_zero___"))
		require.NoError(t, f.keeper.Member.Set(f.ctx, reporter.String(), types.Member{
			Address:        reporter.String(),
			Status:         types.MemberStatus_MEMBER_STATUS_ACTIVE,
			DreamBalance:   keeper.PtrInt(math.NewInt(1000)),
			StakedDream:    keeper.PtrInt(math.NewInt(500)),
			LifetimeEarned: keeper.PtrInt(math.NewInt(1000)),
			LifetimeBurned: keeper.PtrInt(math.ZeroInt()),
			TrustLevel:     types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		}))
		reporterStr, err := f.addressCodec.BytesToString(reporter)
		require.NoError(t, err)

		// Member to be zeroed needs to exist in the member store.
		member := sdk.AccAddress([]byte("subj_zero_______"))
		require.NoError(t, f.keeper.Member.Set(f.ctx, member.String(), types.Member{
			Address:          member.String(),
			Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
			DreamBalance:     keeper.PtrInt(math.NewInt(500)),
			StakedDream:      keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.NewInt(500)),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"backend": "100.0"},
			TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		}))
		memberStr, err := f.addressCodec.BytesToString(member)
		require.NoError(t, err)

		require.NoError(t, f.keeper.MemberReport.Set(f.ctx, memberStr, types.MemberReport{
			Member:    memberStr,
			Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_ESCALATED,
			Reporters: []string{reporterStr},
			TotalBond: "100",
		}))

		_, err = ms.ResolveMemberReport(f.ctx, &types.MsgResolveMemberReport{
			Creator: authorityStr,
			Member:  memberStr,
			Action:  uint64(types.GovActionType_GOV_ACTION_TYPE_ZEROING),
			Reason:  "gross misconduct",
		})
		require.NoError(t, err)

		// Member should now be zeroed.
		memberRec, err := f.keeper.Member.Get(f.ctx, member.String())
		require.NoError(t, err)
		require.Equal(t, types.MemberStatus_MEMBER_STATUS_ZEROED, memberRec.Status)
	})
}
