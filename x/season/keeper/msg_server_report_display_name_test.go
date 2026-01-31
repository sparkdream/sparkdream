package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerReportDisplayName(t *testing.T) {
	t.Run("invalid reporter address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.ReportDisplayName(f.ctx, &types.MsgReportDisplayName{
			Creator: "invalid-address",
			Target:  TestAddrTarget.String(),
			Reason:  TestReportReason,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid reporter address")
	})

	t.Run("invalid target address", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		ms := keeper.NewMsgServerImpl(f.keeper)

		reporter := TestAddrReporter
		reporterStr, _ := f.addressCodec.BytesToString(reporter)

		SetupBasicMemberProfile(t, f.keeper, ctx, reporter)

		_, err := ms.ReportDisplayName(ctx, &types.MsgReportDisplayName{
			Creator: reporterStr,
			Target:  "invalid-address",
			Reason:  TestReportReason,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid target address")
	})

	t.Run("cannot report own display name", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		reporter := TestAddrReporter
		reporterStr, _ := f.addressCodec.BytesToString(reporter)

		SetupMemberProfile(t, k, ctx, reporter, "MyDisplayName", "")

		_, err := ms.ReportDisplayName(ctx, &types.MsgReportDisplayName{
			Creator: reporterStr,
			Target:  reporterStr, // Same as creator
			Reason:  TestReportReason,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrCannotReportOwnDisplayName)
	})

	t.Run("target profile not found", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		reporter := TestAddrReporter
		target := TestAddrTarget
		reporterStr, _ := f.addressCodec.BytesToString(reporter)
		targetStr, _ := f.addressCodec.BytesToString(target)

		SetupBasicMemberProfile(t, k, ctx, reporter)
		// Don't setup target profile

		_, err := ms.ReportDisplayName(ctx, &types.MsgReportDisplayName{
			Creator: reporterStr,
			Target:  targetStr,
			Reason:  TestReportReason,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "profile not found")
	})

	t.Run("target has no display name", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		reporter := TestAddrReporter
		target := TestAddrTarget
		reporterStr, _ := f.addressCodec.BytesToString(reporter)
		targetStr, _ := f.addressCodec.BytesToString(target)

		SetupBasicMemberProfile(t, k, ctx, reporter)
		// Setup target with empty display name
		SetupMemberProfile(t, k, ctx, target, "", "")

		_, err := ms.ReportDisplayName(ctx, &types.MsgReportDisplayName{
			Creator: reporterStr,
			Target:  targetStr,
			Reason:  TestReportReason,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "no display name")
	})

	t.Run("target already moderated", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		reporter := TestAddrReporter
		target := TestAddrTarget
		reporterStr, _ := f.addressCodec.BytesToString(reporter)
		targetStr, _ := f.addressCodec.BytesToString(target)

		SetupBasicMemberProfile(t, k, ctx, reporter)
		SetupMemberProfile(t, k, ctx, target, "BadName", "")

		// Setup existing moderation record
		SetupDisplayNameModeration(t, k, ctx, target, "BadName")

		_, err := ms.ReportDisplayName(ctx, &types.MsgReportDisplayName{
			Creator: reporterStr,
			Target:  targetStr,
			Reason:  TestReportReason,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrDisplayNameModerated)
	})

	t.Run("successful report", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		reporter := TestAddrReporter
		target := TestAddrTarget
		reporterStr, _ := f.addressCodec.BytesToString(reporter)
		targetStr, _ := f.addressCodec.BytesToString(target)

		SetupBasicMemberProfile(t, k, ctx, reporter)
		SetupMemberProfile(t, k, ctx, target, "InappropriateName", "")

		_, err := ms.ReportDisplayName(ctx, &types.MsgReportDisplayName{
			Creator: reporterStr,
			Target:  targetStr,
			Reason:  TestReportReason,
		})

		require.NoError(t, err)

		// Verify moderation record was created
		moderation, err := k.DisplayNameModeration.Get(ctx, targetStr)
		require.NoError(t, err)
		require.Equal(t, targetStr, moderation.Member)
		require.Equal(t, "InappropriateName", moderation.RejectedName)
		require.Equal(t, TestReportReason, moderation.Reason)
		require.True(t, moderation.Active)
		require.Equal(t, ctx.BlockHeight(), moderation.ModeratedAt)

		// Verify target's display name was cleared
		profile, err := k.MemberProfile.Get(ctx, targetStr)
		require.NoError(t, err)
		require.Equal(t, "", profile.DisplayName)
	})

	t.Run("successful report creates stake record", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		reporter := TestAddrMember1
		target := TestAddrMember2
		reporterStr, _ := f.addressCodec.BytesToString(reporter)
		targetStr, _ := f.addressCodec.BytesToString(target)

		SetupBasicMemberProfile(t, k, ctx, reporter)
		SetupMemberProfile(t, k, ctx, target, "ReportedName", "")

		_, err := ms.ReportDisplayName(ctx, &types.MsgReportDisplayName{
			Creator: reporterStr,
			Target:  targetStr,
			Reason:  "Offensive content",
		})

		require.NoError(t, err)

		// Verify stake record exists (challenge ID format: dn:<target>:<block>)
		expectedChallengeID := "dn:" + targetStr + ":0"
		stakeRecord, err := k.DisplayNameReportStake.Get(ctx, expectedChallengeID)
		require.NoError(t, err)
		require.Equal(t, reporterStr, stakeRecord.Reporter)
		require.Equal(t, expectedChallengeID, stakeRecord.ChallengeId)
	})

	t.Run("inactive moderation does not block new report", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		reporter := TestAddrReporter
		target := TestAddrMember3
		reporterStr, _ := f.addressCodec.BytesToString(reporter)
		targetStr, _ := f.addressCodec.BytesToString(target)

		SetupBasicMemberProfile(t, k, ctx, reporter)
		SetupMemberProfile(t, k, ctx, target, "NewBadName", "")

		// Setup inactive moderation record (resolved appeal)
		oldModeration := types.DisplayNameModeration{
			Member:          targetStr,
			RejectedName:    "OldBadName",
			Reason:          "Old reason",
			ModeratedAt:     0,
			Active:          false, // Inactive
			AppealSucceeded: true,
		}
		k.DisplayNameModeration.Set(ctx, targetStr, oldModeration)

		// New report should succeed
		_, err := ms.ReportDisplayName(ctx, &types.MsgReportDisplayName{
			Creator: reporterStr,
			Target:  targetStr,
			Reason:  "New offensive content",
		})

		require.NoError(t, err)

		// Verify new moderation record
		moderation, err := k.DisplayNameModeration.Get(ctx, targetStr)
		require.NoError(t, err)
		require.True(t, moderation.Active)
		require.Equal(t, "NewBadName", moderation.RejectedName)
	})
}
