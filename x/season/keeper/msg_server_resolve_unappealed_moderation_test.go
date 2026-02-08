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

func TestMsgServerResolveUnappealedModeration(t *testing.T) {
	t.Run("happy path - unappealed moderation resolved", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())
		targetStr, _ := f.addressCodec.BytesToString(TestAddrTarget)
		reporterStr, _ := f.addressCodec.BytesToString(TestAddrReporter)

		// Set up active moderation with NO appeal, at block 100
		ctx = ctx.WithBlockHeight(100)
		moderation := types.DisplayNameModeration{
			Member:       targetStr,
			RejectedName: "BadName",
			Reason:       TestReportReason,
			ModeratedAt:  100,
			Active:       true,
			// AppealChallengeId is empty — no appeal filed
		}
		k.DisplayNameModeration.Set(ctx, targetStr, moderation)

		// Set up reporter stake
		reportChallengeID := fmt.Sprintf("dn:%s:%d", targetStr, int64(100))
		reportStake := types.DisplayNameReportStake{
			ChallengeId: reportChallengeID,
			Reporter:    reporterStr,
			Amount:      math.NewInt(50),
		}
		k.DisplayNameReportStake.Set(ctx, reportChallengeID, reportStake)

		// Advance past appeal period (default 100800 blocks)
		params, _ := k.Params.Get(ctx)
		ctx = ctx.WithBlockHeight(100 + int64(params.DisplayNameAppealPeriodBlocks) + 1)

		// Resolve
		_, err := ms.ResolveUnappealedModeration(ctx, &types.MsgResolveUnappealedModeration{
			Authority: authority,
			Member:    targetStr,
		})
		require.NoError(t, err)

		// Verify moderation is now inactive
		updatedMod, err := k.DisplayNameModeration.Get(ctx, targetStr)
		require.NoError(t, err)
		require.False(t, updatedMod.Active)
		require.False(t, updatedMod.AppealSucceeded)

		// Verify report stake was cleaned up
		_, err = k.DisplayNameReportStake.Get(ctx, reportChallengeID)
		require.Error(t, err, "report stake should be removed")
	})

	t.Run("invalid authority address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.ResolveUnappealedModeration(f.ctx, &types.MsgResolveUnappealedModeration{
			Authority: "invalid-address",
			Member:    TestAddrTarget.String(),
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid authority address")
	})

	t.Run("not authorized", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		ms := keeper.NewMsgServerImpl(f.keeper)

		nonAuthority, _ := f.addressCodec.BytesToString(TestAddrCreator)

		_, err := ms.ResolveUnappealedModeration(ctx, &types.MsgResolveUnappealedModeration{
			Authority: nonAuthority,
			Member:    TestAddrTarget.String(),
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

		_, err := ms.ResolveUnappealedModeration(ctx, &types.MsgResolveUnappealedModeration{
			Authority: authority,
			Member:    targetStr,
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

		moderation := types.DisplayNameModeration{
			Member:       targetStr,
			RejectedName: "Name",
			ModeratedAt:  0,
			Active:       false, // already resolved
		}
		k.DisplayNameModeration.Set(ctx, targetStr, moderation)

		_, err := ms.ResolveUnappealedModeration(ctx, &types.MsgResolveUnappealedModeration{
			Authority: authority,
			Member:    targetStr,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrAppealAlreadyResolved)
	})

	t.Run("appeal exists - should use ResolveDisplayNameAppeal instead", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())
		targetStr, _ := f.addressCodec.BytesToString(TestAddrTarget)

		moderation := types.DisplayNameModeration{
			Member:            targetStr,
			RejectedName:      "Name",
			ModeratedAt:       0,
			Active:            true,
			AppealChallengeId: "dn_appeal:test:0", // appeal exists
		}
		k.DisplayNameModeration.Set(ctx, targetStr, moderation)

		// Advance past appeal period
		ctx = ctx.WithBlockHeight(200000)

		_, err := ms.ResolveUnappealedModeration(ctx, &types.MsgResolveUnappealedModeration{
			Authority: authority,
			Member:    targetStr,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrAppealAlreadySubmitted)
	})

	t.Run("appeal period not expired", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())
		targetStr, _ := f.addressCodec.BytesToString(TestAddrTarget)

		moderation := types.DisplayNameModeration{
			Member:       targetStr,
			RejectedName: "Name",
			ModeratedAt:  100,
			Active:       true,
		}
		k.DisplayNameModeration.Set(ctx, targetStr, moderation)

		// Still within appeal period
		ctx = ctx.WithBlockHeight(200)

		_, err := ms.ResolveUnappealedModeration(ctx, &types.MsgResolveUnappealedModeration{
			Authority: authority,
			Member:    targetStr,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrAppealPeriodNotExpired)
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

		moderation := types.DisplayNameModeration{
			Member:       targetStr,
			RejectedName: "Name",
			ModeratedAt:  0,
			Active:       true,
		}
		k.DisplayNameModeration.Set(ctx, targetStr, moderation)

		// Advance past appeal period
		params, _ := k.Params.Get(ctx)
		ctx = ctx.WithBlockHeight(int64(params.DisplayNameAppealPeriodBlocks) + 1)

		_, err := ms.ResolveUnappealedModeration(ctx, &types.MsgResolveUnappealedModeration{
			Authority: addrStr,
			Member:    targetStr,
		})
		require.NoError(t, err)

		updatedMod, err := k.DisplayNameModeration.Get(ctx, targetStr)
		require.NoError(t, err)
		require.False(t, updatedMod.Active)
	})
}

func TestBeginBlocker_ProcessExpiredModerations(t *testing.T) {
	t.Run("auto-resolves expired unappealed moderation", func(t *testing.T) {
		f := initBeginBlockFixture(t)

		targetStr, _ := f.addressCodec.BytesToString(TestAddrTarget)
		reporterStr, _ := f.addressCodec.BytesToString(TestAddrReporter)

		// Create an active season so BeginBlocker doesn't fail
		season := types.Season{
			Number:     1,
			Name:       "Test Season",
			StartBlock: 0,
			EndBlock:   1000000,
			Status:     types.SeasonStatus_SEASON_STATUS_ACTIVE,
		}
		f.keeper.Season.Set(f.ctx, season)

		// Set up active moderation with NO appeal at block 100
		moderation := types.DisplayNameModeration{
			Member:       targetStr,
			RejectedName: "BadName",
			Reason:       TestReportReason,
			ModeratedAt:  100,
			Active:       true,
		}
		f.keeper.DisplayNameModeration.Set(f.ctx, targetStr, moderation)

		// Set up reporter stake
		reportChallengeID := fmt.Sprintf("dn:%s:%d", targetStr, int64(100))
		reportStake := types.DisplayNameReportStake{
			ChallengeId: reportChallengeID,
			Reporter:    reporterStr,
			Amount:      math.NewInt(50),
		}
		f.keeper.DisplayNameReportStake.Set(f.ctx, reportChallengeID, reportStake)

		// Advance past appeal period
		params, _ := f.keeper.Params.Get(f.ctx)
		f.ctx = f.ctx.WithBlockHeight(100 + int64(params.DisplayNameAppealPeriodBlocks) + 1)

		// Call BeginBlocker — should auto-resolve
		err := f.keeper.BeginBlocker(f.ctx)
		require.NoError(t, err)

		// Verify moderation was resolved
		updatedMod, err := f.keeper.DisplayNameModeration.Get(f.ctx, targetStr)
		require.NoError(t, err)
		require.False(t, updatedMod.Active, "moderation should be auto-resolved")
		require.False(t, updatedMod.AppealSucceeded, "report upheld, appeal not succeeded")

		// Verify report stake was cleaned up
		_, err = f.keeper.DisplayNameReportStake.Get(f.ctx, reportChallengeID)
		require.Error(t, err, "report stake should be removed after auto-resolution")
	})

	t.Run("does not resolve moderation within appeal period", func(t *testing.T) {
		f := initBeginBlockFixture(t)

		targetStr, _ := f.addressCodec.BytesToString(TestAddrTarget)

		season := types.Season{
			Number:     1,
			Name:       "Test Season",
			StartBlock: 0,
			EndBlock:   1000000,
			Status:     types.SeasonStatus_SEASON_STATUS_ACTIVE,
		}
		f.keeper.Season.Set(f.ctx, season)

		moderation := types.DisplayNameModeration{
			Member:       targetStr,
			RejectedName: "BadName",
			Reason:       TestReportReason,
			ModeratedAt:  100,
			Active:       true,
		}
		f.keeper.DisplayNameModeration.Set(f.ctx, targetStr, moderation)

		// Still within appeal period
		f.ctx = f.ctx.WithBlockHeight(200)

		err := f.keeper.BeginBlocker(f.ctx)
		require.NoError(t, err)

		// Moderation should still be active
		mod, err := f.keeper.DisplayNameModeration.Get(f.ctx, targetStr)
		require.NoError(t, err)
		require.True(t, mod.Active, "moderation should remain active during appeal period")
	})

	t.Run("skips moderations that have an appeal", func(t *testing.T) {
		f := initBeginBlockFixture(t)

		targetStr, _ := f.addressCodec.BytesToString(TestAddrTarget)

		season := types.Season{
			Number:     1,
			Name:       "Test Season",
			StartBlock: 0,
			EndBlock:   1000000,
			Status:     types.SeasonStatus_SEASON_STATUS_ACTIVE,
		}
		f.keeper.Season.Set(f.ctx, season)

		moderation := types.DisplayNameModeration{
			Member:            targetStr,
			RejectedName:      "BadName",
			Reason:            TestReportReason,
			ModeratedAt:       100,
			Active:            true,
			AppealChallengeId: "dn_appeal:test:150", // has appeal
		}
		f.keeper.DisplayNameModeration.Set(f.ctx, targetStr, moderation)

		// Advance well past appeal period
		f.ctx = f.ctx.WithBlockHeight(200000)

		err := f.keeper.BeginBlocker(f.ctx)
		require.NoError(t, err)

		// Moderation should still be active (has appeal, must be resolved via ResolveDisplayNameAppeal)
		mod, err := f.keeper.DisplayNameModeration.Get(f.ctx, targetStr)
		require.NoError(t, err)
		require.True(t, mod.Active, "moderation with appeal should not be auto-resolved")
	})

	t.Run("skips already resolved moderations", func(t *testing.T) {
		f := initBeginBlockFixture(t)

		targetStr, _ := f.addressCodec.BytesToString(TestAddrTarget)

		season := types.Season{
			Number:     1,
			Name:       "Test Season",
			StartBlock: 0,
			EndBlock:   1000000,
			Status:     types.SeasonStatus_SEASON_STATUS_ACTIVE,
		}
		f.keeper.Season.Set(f.ctx, season)

		moderation := types.DisplayNameModeration{
			Member:       targetStr,
			RejectedName: "OldName",
			ModeratedAt:  0,
			Active:       false, // already resolved
		}
		f.keeper.DisplayNameModeration.Set(f.ctx, targetStr, moderation)

		f.ctx = f.ctx.WithBlockHeight(200000)

		// Should not error
		err := f.keeper.BeginBlocker(f.ctx)
		require.NoError(t, err)

		// Should remain unchanged
		mod, err := f.keeper.DisplayNameModeration.Get(f.ctx, targetStr)
		require.NoError(t, err)
		require.False(t, mod.Active)
	})
}
