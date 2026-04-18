package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// setSentinelReporter seeds a member with enough reputation and staked DREAM
// to pass the sentinel reporter-bond checks.
func setSentinelReporter(t *testing.T, k keeper.Keeper, ctx sdk.Context, addr sdk.AccAddress) {
	t.Helper()
	// Tier 3 requires total reputation >= 200. Give 250 to clear the bar.
	// StakedDream >= DefaultMinSentinelBond (500 DREAM).
	err := k.Member.Set(ctx, addr.String(), types.Member{
		Address:          addr.String(),
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:     keeper.PtrInt(math.NewInt(2000)),
		StakedDream:      keeper.PtrInt(math.NewInt(500)),
		LifetimeEarned:   keeper.PtrInt(math.NewInt(2000)),
		LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "250.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})
	require.NoError(t, err)
}

func TestMsgServerReportMember(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		member := sdk.AccAddress([]byte("member__________"))
		memberStr, err := f.addressCodec.BytesToString(member)
		require.NoError(t, err)

		_, err = ms.ReportMember(f.ctx, &types.MsgReportMember{
			Creator: "invalid-address",
			Member:  memberStr,
			Reason:  "spam",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("invalid member address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		reporter := sdk.AccAddress([]byte("reporter________"))
		reporterStr, err := f.addressCodec.BytesToString(reporter)
		require.NoError(t, err)

		_, err = ms.ReportMember(f.ctx, &types.MsgReportMember{
			Creator: reporterStr,
			Member:  "invalid-address",
			Reason:  "spam",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid member address")
	})

	t.Run("cannot report self", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		reporter := sdk.AccAddress([]byte("self_reporter___"))
		reporterStr, err := f.addressCodec.BytesToString(reporter)
		require.NoError(t, err)

		setSentinelReporter(t, f.keeper, f.ctx, reporter)

		_, err = ms.ReportMember(f.ctx, &types.MsgReportMember{
			Creator: reporterStr,
			Member:  reporterStr,
			Reason:  "self",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrCannotReportSelf)
	})

	t.Run("insufficient reputation tier", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		// Reporter with reputation below tier 3.
		reporter := sdk.AccAddress([]byte("low_rep_reporter"))
		err := f.keeper.Member.Set(f.ctx, reporter.String(), types.Member{
			Address:          reporter.String(),
			Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
			DreamBalance:     keeper.PtrInt(math.NewInt(2000)),
			StakedDream:      keeper.PtrInt(math.NewInt(500)),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"backend": "5.0"},
			TrustLevel:       types.TrustLevel_TRUST_LEVEL_NEW,
		})
		require.NoError(t, err)

		reporterStr, err := f.addressCodec.BytesToString(reporter)
		require.NoError(t, err)
		member := sdk.AccAddress([]byte("subject_phoenix_"))
		memberStr, err := f.addressCodec.BytesToString(member)
		require.NoError(t, err)

		_, err = ms.ReportMember(f.ctx, &types.MsgReportMember{
			Creator: reporterStr,
			Member:  memberStr,
			Reason:  "spam",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInsufficientReputation)
	})

	t.Run("insufficient sentinel bond", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		// Reporter has enough reputation but not enough staked DREAM.
		reporter := sdk.AccAddress([]byte("low_bond_rep____"))
		err := f.keeper.Member.Set(f.ctx, reporter.String(), types.Member{
			Address:          reporter.String(),
			Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
			DreamBalance:     keeper.PtrInt(math.NewInt(2000)),
			StakedDream:      keeper.PtrInt(math.NewInt(100)),
			LifetimeEarned:   keeper.PtrInt(math.NewInt(2000)),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"backend": "500.0"},
			TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		})
		require.NoError(t, err)

		reporterStr, err := f.addressCodec.BytesToString(reporter)
		require.NoError(t, err)
		member := sdk.AccAddress([]byte("subject_aurora__"))
		memberStr, err := f.addressCodec.BytesToString(member)
		require.NoError(t, err)

		_, err = ms.ReportMember(f.ctx, &types.MsgReportMember{
			Creator: reporterStr,
			Member:  memberStr,
			Reason:  "spam",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInsufficientSentinelBond)
	})

	t.Run("report already exists", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		reporter := sdk.AccAddress([]byte("reporter_exist__"))
		setSentinelReporter(t, f.keeper, f.ctx, reporter)
		reporterStr, err := f.addressCodec.BytesToString(reporter)
		require.NoError(t, err)

		member := sdk.AccAddress([]byte("subject_zenith__"))
		memberStr, err := f.addressCodec.BytesToString(member)
		require.NoError(t, err)

		// Seed an existing report.
		err = f.keeper.MemberReport.Set(f.ctx, memberStr, types.MemberReport{
			Member: memberStr,
			Status: types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
		})
		require.NoError(t, err)

		_, err = ms.ReportMember(f.ctx, &types.MsgReportMember{
			Creator: reporterStr,
			Member:  memberStr,
			Reason:  "spam",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrReportAlreadyExists)
	})

	t.Run("successful report", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		reporter := sdk.AccAddress([]byte("reporter_happy__"))
		setSentinelReporter(t, f.keeper, f.ctx, reporter)
		reporterStr, err := f.addressCodec.BytesToString(reporter)
		require.NoError(t, err)

		member := sdk.AccAddress([]byte("subject_happy___"))
		memberStr, err := f.addressCodec.BytesToString(member)
		require.NoError(t, err)

		_, err = ms.ReportMember(f.ctx, &types.MsgReportMember{
			Creator:           reporterStr,
			Member:            memberStr,
			Reason:            "spam and abuse",
			RecommendedAction: uint64(types.GovActionType_GOV_ACTION_TYPE_WARNING),
		})
		require.NoError(t, err)

		// Report exists with expected fields.
		report, err := f.keeper.MemberReport.Get(f.ctx, memberStr)
		require.NoError(t, err)
		require.Equal(t, memberStr, report.Member)
		require.Equal(t, "spam and abuse", report.Reason)
		require.Equal(t, types.GovActionType_GOV_ACTION_TYPE_WARNING, report.RecommendedAction)
		require.Equal(t, types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING, report.Status)
		require.Equal(t, []string{reporterStr}, report.Reporters)
		require.Equal(t, "500", report.TotalBond) // min(500 staked, 1000 cap) = 500

		// An event should be emitted.
		events := f.ctx.EventManager().Events()
		found := false
		for _, e := range events {
			if e.Type == "member_reported" {
				found = true
			}
		}
		require.True(t, found, "expected member_reported event")
	})

	t.Run("default action when unspecified", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		reporter := sdk.AccAddress([]byte("reporter_default"))
		setSentinelReporter(t, f.keeper, f.ctx, reporter)
		reporterStr, err := f.addressCodec.BytesToString(reporter)
		require.NoError(t, err)

		member := sdk.AccAddress([]byte("subject_default_"))
		memberStr, err := f.addressCodec.BytesToString(member)
		require.NoError(t, err)

		_, err = ms.ReportMember(f.ctx, &types.MsgReportMember{
			Creator:           reporterStr,
			Member:            memberStr,
			Reason:            "ambiguous",
			RecommendedAction: uint64(types.GovActionType_GOV_ACTION_TYPE_UNSPECIFIED),
		})
		require.NoError(t, err)

		report, err := f.keeper.MemberReport.Get(f.ctx, memberStr)
		require.NoError(t, err)
		// Handler defaults unspecified -> WARNING.
		require.Equal(t, types.GovActionType_GOV_ACTION_TYPE_WARNING, report.RecommendedAction)
	})
}
