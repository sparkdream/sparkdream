package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerCosignMemberReport(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		member := sdk.AccAddress([]byte("subject_invalid_"))
		memberStr, err := f.addressCodec.BytesToString(member)
		require.NoError(t, err)

		_, err = ms.CosignMemberReport(f.ctx, &types.MsgCosignMemberReport{
			Creator: "invalid",
			Member:  memberStr,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("report not found", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		cosigner := sdk.AccAddress([]byte("cosigner_missing"))
		setSentinelReporter(t, f.keeper, f.ctx, cosigner)
		cosignerStr, err := f.addressCodec.BytesToString(cosigner)
		require.NoError(t, err)

		missing := sdk.AccAddress([]byte("subject_missing_"))
		missingStr, err := f.addressCodec.BytesToString(missing)
		require.NoError(t, err)

		_, err = ms.CosignMemberReport(f.ctx, &types.MsgCosignMemberReport{
			Creator: cosignerStr,
			Member:  missingStr,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrReportNotFound)
	})

	t.Run("report not pending", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		cosigner := sdk.AccAddress([]byte("cosigner_resolv_"))
		setSentinelReporter(t, f.keeper, f.ctx, cosigner)
		cosignerStr, err := f.addressCodec.BytesToString(cosigner)
		require.NoError(t, err)

		member := sdk.AccAddress([]byte("subject_resolved"))
		memberStr, err := f.addressCodec.BytesToString(member)
		require.NoError(t, err)

		err = f.keeper.MemberReport.Set(f.ctx, memberStr, types.MemberReport{
			Member: memberStr,
			Status: types.MemberReportStatus_MEMBER_REPORT_STATUS_RESOLVED,
		})
		require.NoError(t, err)

		_, err = ms.CosignMemberReport(f.ctx, &types.MsgCosignMemberReport{
			Creator: cosignerStr,
			Member:  memberStr,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrReportNotPending)
	})

	t.Run("insufficient reputation", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		cosigner := sdk.AccAddress([]byte("cosigner_low_rep"))
		// low reputation (<tier 3)
		require.NoError(t, f.keeper.Member.Set(f.ctx, cosigner.String(), types.Member{
			Address:          cosigner.String(),
			Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
			DreamBalance:     keeper.PtrInt(math.NewInt(2000)),
			StakedDream:      keeper.PtrInt(math.NewInt(500)),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"backend": "5.0"},
			TrustLevel:       types.TrustLevel_TRUST_LEVEL_NEW,
		}))
		cosignerStr, err := f.addressCodec.BytesToString(cosigner)
		require.NoError(t, err)

		member := sdk.AccAddress([]byte("subject_low_rep_"))
		memberStr, err := f.addressCodec.BytesToString(member)
		require.NoError(t, err)

		require.NoError(t, f.keeper.MemberReport.Set(f.ctx, memberStr, types.MemberReport{
			Member:    memberStr,
			Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
			Reporters: []string{"existing-reporter"},
			TotalBond: "500",
		}))

		_, err = ms.CosignMemberReport(f.ctx, &types.MsgCosignMemberReport{
			Creator: cosignerStr,
			Member:  memberStr,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInsufficientReputation)
	})

	t.Run("already cosigned", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		cosigner := sdk.AccAddress([]byte("cosigner_dup____"))
		setSentinelReporter(t, f.keeper, f.ctx, cosigner)
		cosignerStr, err := f.addressCodec.BytesToString(cosigner)
		require.NoError(t, err)

		member := sdk.AccAddress([]byte("subject_dup_____"))
		memberStr, err := f.addressCodec.BytesToString(member)
		require.NoError(t, err)

		// Report lists cosigner as an existing reporter.
		require.NoError(t, f.keeper.MemberReport.Set(f.ctx, memberStr, types.MemberReport{
			Member:    memberStr,
			Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
			Reporters: []string{cosignerStr},
			TotalBond: "500",
		}))

		_, err = ms.CosignMemberReport(f.ctx, &types.MsgCosignMemberReport{
			Creator: cosignerStr,
			Member:  memberStr,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrAlreadyCosigned)
	})

	t.Run("successful cosign", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		cosigner := sdk.AccAddress([]byte("cosigner_happy__"))
		setSentinelReporter(t, f.keeper, f.ctx, cosigner)
		cosignerStr, err := f.addressCodec.BytesToString(cosigner)
		require.NoError(t, err)

		member := sdk.AccAddress([]byte("subject_cosign__"))
		memberStr, err := f.addressCodec.BytesToString(member)
		require.NoError(t, err)

		require.NoError(t, f.keeper.MemberReport.Set(f.ctx, memberStr, types.MemberReport{
			Member:    memberStr,
			Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
			Reporters: []string{"existing-reporter"},
			TotalBond: "500",
		}))

		_, err = ms.CosignMemberReport(f.ctx, &types.MsgCosignMemberReport{
			Creator: cosignerStr,
			Member:  memberStr,
		})
		require.NoError(t, err)

		report, err := f.keeper.MemberReport.Get(f.ctx, memberStr)
		require.NoError(t, err)
		require.Len(t, report.Reporters, 2)
		require.Equal(t, cosignerStr, report.Reporters[1])
		// Still pending since only 2 reporters (< cosign threshold of 3).
		require.Equal(t, types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING, report.Status)
		// total bond: 500 + 500 = 1000
		require.Equal(t, "1000", report.TotalBond)
	})

	t.Run("escalates at cosign threshold", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		cosigner := sdk.AccAddress([]byte("cosigner_escal__"))
		setSentinelReporter(t, f.keeper, f.ctx, cosigner)
		cosignerStr, err := f.addressCodec.BytesToString(cosigner)
		require.NoError(t, err)

		member := sdk.AccAddress([]byte("subject_escal___"))
		memberStr, err := f.addressCodec.BytesToString(member)
		require.NoError(t, err)

		// Already two reporters; cosigning makes three -> escalated.
		require.NoError(t, f.keeper.MemberReport.Set(f.ctx, memberStr, types.MemberReport{
			Member:    memberStr,
			Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
			Reporters: []string{"reporter-1", "reporter-2"},
			TotalBond: "1000",
		}))

		_, err = ms.CosignMemberReport(f.ctx, &types.MsgCosignMemberReport{
			Creator: cosignerStr,
			Member:  memberStr,
		})
		require.NoError(t, err)

		report, err := f.keeper.MemberReport.Get(f.ctx, memberStr)
		require.NoError(t, err)
		require.Equal(t, types.MemberReportStatus_MEMBER_REPORT_STATUS_ESCALATED, report.Status)
	})
}
