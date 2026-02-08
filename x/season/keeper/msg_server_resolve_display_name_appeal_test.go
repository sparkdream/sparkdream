package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerResolveDisplayNameAppeal(t *testing.T) {
	t.Run("invalid authority address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.ResolveDisplayNameAppeal(f.ctx, &types.MsgResolveDisplayNameAppeal{
			Authority:       "invalid-address",
			Member:          TestAddrTarget.String(),
			AppealSucceeded: true,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid authority address")
	})

	t.Run("not authorized - no commons keeper", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		ms := keeper.NewMsgServerImpl(f.keeper)

		nonAuthority := TestAddrCreator
		nonAuthorityStr, _ := f.addressCodec.BytesToString(nonAuthority)

		_, err := ms.ResolveDisplayNameAppeal(ctx, &types.MsgResolveDisplayNameAppeal{
			Authority:       nonAuthorityStr,
			Member:          TestAddrTarget.String(),
			AppealSucceeded: true,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotAuthorized)
	})

	t.Run("no moderation record", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())
		targetStr, _ := f.addressCodec.BytesToString(TestAddrTarget)

		_, err := ms.ResolveDisplayNameAppeal(ctx, &types.MsgResolveDisplayNameAppeal{
			Authority:       authority,
			Member:          targetStr,
			AppealSucceeded: true,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrDisplayNameNotModerated)
	})

	t.Run("moderation already resolved", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())
		targetStr, _ := f.addressCodec.BytesToString(TestAddrTarget)

		// Set up an inactive (already resolved) moderation record
		moderation := types.DisplayNameModeration{
			Member:            targetStr,
			RejectedName:      "OldBadName",
			Reason:            TestReportReason,
			ModeratedAt:       0,
			Active:            false,
			AppealChallengeId: "dn_appeal:test:0",
			AppealSucceeded:   true,
		}
		k.DisplayNameModeration.Set(ctx, targetStr, moderation)

		_, err := ms.ResolveDisplayNameAppeal(ctx, &types.MsgResolveDisplayNameAppeal{
			Authority:       authority,
			Member:          targetStr,
			AppealSucceeded: true,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrAppealAlreadyResolved)
	})

	t.Run("no appeal to resolve", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())
		targetStr, _ := f.addressCodec.BytesToString(TestAddrTarget)

		// Set up an active moderation with no appeal
		moderation := types.DisplayNameModeration{
			Member:       targetStr,
			RejectedName: "BadName",
			Reason:       TestReportReason,
			ModeratedAt:  0,
			Active:       true,
			// AppealChallengeId is empty - no appeal filed
		}
		k.DisplayNameModeration.Set(ctx, targetStr, moderation)

		_, err := ms.ResolveDisplayNameAppeal(ctx, &types.MsgResolveDisplayNameAppeal{
			Authority:       authority,
			Member:          targetStr,
			AppealSucceeded: true,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNoAppealToResolve)
	})

	t.Run("appeal succeeds - name restored", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())
		targetStr, _ := f.addressCodec.BytesToString(TestAddrTarget)
		reporterStr, _ := f.addressCodec.BytesToString(TestAddrReporter)

		// Set up member profile with cleared display name
		SetupMemberProfile(t, k, ctx, TestAddrTarget, "", "")

		// Set up active moderation with appeal
		blockHeight := ctx.BlockHeight()
		reportChallengeID := fmt.Sprintf("dn:%s:%d", targetStr, blockHeight)
		appealChallengeID := fmt.Sprintf("dn_appeal:%s:%d", targetStr, blockHeight)

		moderation := types.DisplayNameModeration{
			Member:            targetStr,
			RejectedName:      "GoodNameActually",
			Reason:            TestReportReason,
			ModeratedAt:       blockHeight,
			Active:            true,
			AppealChallengeId: appealChallengeID,
			AppealedAt:        blockHeight,
		}
		k.DisplayNameModeration.Set(ctx, targetStr, moderation)

		// Set up stake records
		reportStake := types.DisplayNameReportStake{
			ChallengeId: reportChallengeID,
			Reporter:    reporterStr,
			Amount:      math.NewInt(50),
		}
		k.DisplayNameReportStake.Set(ctx, reportChallengeID, reportStake)

		appealStake := types.DisplayNameAppealStake{
			ChallengeId: appealChallengeID,
			Appellant:   targetStr,
			Amount:      math.NewInt(100),
		}
		k.DisplayNameAppealStake.Set(ctx, appealChallengeID, appealStake)

		// Resolve: appeal succeeds
		_, err := ms.ResolveDisplayNameAppeal(ctx, &types.MsgResolveDisplayNameAppeal{
			Authority:       authority,
			Member:          targetStr,
			AppealSucceeded: true,
		})

		require.NoError(t, err)

		// Verify moderation record updated
		updatedMod, err := k.DisplayNameModeration.Get(ctx, targetStr)
		require.NoError(t, err)
		require.False(t, updatedMod.Active, "moderation should be inactive after resolution")
		require.True(t, updatedMod.AppealSucceeded, "appeal should be marked as succeeded")

		// Verify display name was restored
		profile, err := k.MemberProfile.Get(ctx, targetStr)
		require.NoError(t, err)
		require.Equal(t, "GoodNameActually", profile.DisplayName, "display name should be restored")

		// Verify stake records were cleaned up
		_, err = k.DisplayNameReportStake.Get(ctx, reportChallengeID)
		require.Error(t, err, "report stake should be removed")

		_, err = k.DisplayNameAppealStake.Get(ctx, appealChallengeID)
		require.Error(t, err, "appeal stake should be removed")
	})

	t.Run("appeal fails - name stays cleared", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())
		targetStr, _ := f.addressCodec.BytesToString(TestAddrTarget)
		reporterStr, _ := f.addressCodec.BytesToString(TestAddrReporter)

		// Set up member profile with cleared display name
		SetupMemberProfile(t, k, ctx, TestAddrTarget, "", "")

		// Set up active moderation with appeal
		blockHeight := ctx.BlockHeight()
		reportChallengeID := fmt.Sprintf("dn:%s:%d", targetStr, blockHeight)
		appealChallengeID := fmt.Sprintf("dn_appeal:%s:%d", targetStr, blockHeight)

		moderation := types.DisplayNameModeration{
			Member:            targetStr,
			RejectedName:      "TrulyBadName",
			Reason:            TestReportReason,
			ModeratedAt:       blockHeight,
			Active:            true,
			AppealChallengeId: appealChallengeID,
			AppealedAt:        blockHeight,
		}
		k.DisplayNameModeration.Set(ctx, targetStr, moderation)

		// Set up stake records
		reportStake := types.DisplayNameReportStake{
			ChallengeId: reportChallengeID,
			Reporter:    reporterStr,
			Amount:      math.NewInt(50),
		}
		k.DisplayNameReportStake.Set(ctx, reportChallengeID, reportStake)

		appealStake := types.DisplayNameAppealStake{
			ChallengeId: appealChallengeID,
			Appellant:   targetStr,
			Amount:      math.NewInt(100),
		}
		k.DisplayNameAppealStake.Set(ctx, appealChallengeID, appealStake)

		// Resolve: appeal fails
		_, err := ms.ResolveDisplayNameAppeal(ctx, &types.MsgResolveDisplayNameAppeal{
			Authority:       authority,
			Member:          targetStr,
			AppealSucceeded: false,
		})

		require.NoError(t, err)

		// Verify moderation record updated
		updatedMod, err := k.DisplayNameModeration.Get(ctx, targetStr)
		require.NoError(t, err)
		require.False(t, updatedMod.Active, "moderation should be inactive after resolution")
		require.False(t, updatedMod.AppealSucceeded, "appeal should be marked as failed")

		// Verify display name stays cleared
		profile, err := k.MemberProfile.Get(ctx, targetStr)
		require.NoError(t, err)
		require.Equal(t, "", profile.DisplayName, "display name should remain cleared")

		// Verify stake records were cleaned up
		_, err = k.DisplayNameReportStake.Get(ctx, reportChallengeID)
		require.Error(t, err, "report stake should be removed")

		_, err = k.DisplayNameAppealStake.Get(ctx, appealChallengeID)
		require.Error(t, err, "appeal stake should be removed")
	})

	t.Run("new report allowed after resolution", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())
		targetStr, _ := f.addressCodec.BytesToString(TestAddrTarget)
		reporterStr, _ := f.addressCodec.BytesToString(TestAddrReporter)

		// Set up profiles
		SetupBasicMemberProfile(t, k, ctx, TestAddrReporter)
		SetupMemberProfile(t, k, ctx, TestAddrTarget, "", "") // cleared by report

		// Set up resolved moderation with appeal (appeal will restore this name)
		blockHeight := ctx.BlockHeight()
		appealChallengeID := fmt.Sprintf("dn_appeal:%s:%d", targetStr, blockHeight)

		moderation := types.DisplayNameModeration{
			Member:            targetStr,
			RejectedName:      "RestoredName",
			Reason:            "old reason",
			ModeratedAt:       blockHeight,
			Active:            true,
			AppealChallengeId: appealChallengeID,
			AppealedAt:        blockHeight,
		}
		k.DisplayNameModeration.Set(ctx, targetStr, moderation)

		// Resolve the appeal first
		_, err := ms.ResolveDisplayNameAppeal(ctx, &types.MsgResolveDisplayNameAppeal{
			Authority:       authority,
			Member:          targetStr,
			AppealSucceeded: true,
		})
		require.NoError(t, err)

		// Verify moderation is now inactive
		resolvedMod, err := k.DisplayNameModeration.Get(ctx, targetStr)
		require.NoError(t, err)
		require.False(t, resolvedMod.Active)

		// Now a new report should succeed (existing report test verifies inactive moderation doesn't block)
		_, err = ms.ReportDisplayName(ctx, &types.MsgReportDisplayName{
			Creator: reporterStr,
			Target:  targetStr,
			Reason:  "New offensive content",
		})

		require.NoError(t, err)

		// Verify new moderation record is active
		newMod, err := k.DisplayNameModeration.Get(ctx, targetStr)
		require.NoError(t, err)
		require.True(t, newMod.Active)
		require.Equal(t, "RestoredName", newMod.RejectedName)
	})

	t.Run("authorized via operations committee", func(t *testing.T) {
		committeeAddr := TestAddrMember1
		committeeAddrStr := committeeAddr.String()

		mockCommons := newMockCommonsKeeper(committeeAddrStr)
		f := initFixtureWithCommons(t, mockCommons)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		addrStr, _ := f.addressCodec.BytesToString(committeeAddr)
		targetStr, _ := f.addressCodec.BytesToString(TestAddrTarget)

		// Set up active moderation with appeal
		appealChallengeID := "dn_appeal:test:0"
		moderation := types.DisplayNameModeration{
			Member:            targetStr,
			RejectedName:      "TestName",
			Reason:            TestReportReason,
			ModeratedAt:       0,
			Active:            true,
			AppealChallengeId: appealChallengeID,
		}
		k.DisplayNameModeration.Set(ctx, targetStr, moderation)

		// Set up member profile
		SetupMemberProfile(t, k, ctx, TestAddrTarget, "", "")

		_, err := ms.ResolveDisplayNameAppeal(ctx, &types.MsgResolveDisplayNameAppeal{
			Authority:       addrStr,
			Member:          targetStr,
			AppealSucceeded: true,
		})

		require.NoError(t, err)

		// Verify resolution
		updatedMod, err := k.DisplayNameModeration.Get(ctx, targetStr)
		require.NoError(t, err)
		require.False(t, updatedMod.Active)
		require.True(t, updatedMod.AppealSucceeded)
	})
}
