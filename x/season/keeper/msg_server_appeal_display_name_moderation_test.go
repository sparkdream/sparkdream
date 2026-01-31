package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerAppealDisplayNameModeration(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.AppealDisplayNameModeration(f.ctx, &types.MsgAppealDisplayNameModeration{
			Creator:      "invalid-address",
			AppealReason: TestAppealReason,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("no moderation record exists", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		// No moderation record exists for this user

		_, err := ms.AppealDisplayNameModeration(ctx, &types.MsgAppealDisplayNameModeration{
			Creator:      creatorStr,
			AppealReason: TestAppealReason,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrDisplayNameNotModerated)
	})

	t.Run("moderation not active", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		// Setup inactive moderation record
		moderation := types.DisplayNameModeration{
			Member:       creatorStr,
			RejectedName: "SomeName",
			Reason:       TestReportReason,
			ModeratedAt:  ctx.BlockHeight(),
			Active:       false, // Not active
		}
		k.DisplayNameModeration.Set(ctx, creatorStr, moderation)

		_, err := ms.AppealDisplayNameModeration(ctx, &types.MsgAppealDisplayNameModeration{
			Creator:      creatorStr,
			AppealReason: TestAppealReason,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrDisplayNameNotModerated)
	})

	t.Run("appeal already submitted", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		// Setup moderation with existing appeal
		moderation := types.DisplayNameModeration{
			Member:            creatorStr,
			RejectedName:      "SomeName",
			Reason:            TestReportReason,
			ModeratedAt:       ctx.BlockHeight(),
			Active:            true,
			AppealChallengeId: "existing-appeal-123", // Already has appeal
		}
		k.DisplayNameModeration.Set(ctx, creatorStr, moderation)

		_, err := ms.AppealDisplayNameModeration(ctx, &types.MsgAppealDisplayNameModeration{
			Creator:      creatorStr,
			AppealReason: TestAppealReason,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrAppealAlreadySubmitted)
	})

	t.Run("appeal period expired", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		params, _ := k.Params.Get(ctx)

		// Setup moderation created long ago
		moderation := types.DisplayNameModeration{
			Member:       creatorStr,
			RejectedName: "SomeName",
			Reason:       TestReportReason,
			ModeratedAt:  0, // Created at block 0
			Active:       true,
		}
		k.DisplayNameModeration.Set(ctx, creatorStr, moderation)

		// Advance time past appeal period
		ctx = ctx.WithBlockHeight(int64(params.DisplayNameAppealPeriodBlocks) + 1)

		_, err := ms.AppealDisplayNameModeration(ctx, &types.MsgAppealDisplayNameModeration{
			Creator:      creatorStr,
			AppealReason: TestAppealReason,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrAppealPeriodExpired)
	})

	t.Run("successful appeal", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrTarget
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		// Setup active moderation record
		moderation := types.DisplayNameModeration{
			Member:       creatorStr,
			RejectedName: "MyDisplayName",
			Reason:       TestReportReason,
			ModeratedAt:  ctx.BlockHeight(),
			Active:       true,
		}
		k.DisplayNameModeration.Set(ctx, creatorStr, moderation)

		_, err := ms.AppealDisplayNameModeration(ctx, &types.MsgAppealDisplayNameModeration{
			Creator:      creatorStr,
			AppealReason: TestAppealReason,
		})

		require.NoError(t, err)

		// Verify moderation was updated with appeal info
		updatedModeration, err := k.DisplayNameModeration.Get(ctx, creatorStr)
		require.NoError(t, err)
		require.NotEmpty(t, updatedModeration.AppealChallengeId)
		require.Equal(t, ctx.BlockHeight(), updatedModeration.AppealedAt)
		require.True(t, updatedModeration.Active) // Still active until resolved
	})

	t.Run("successful appeal creates stake record", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrMember1
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		// Setup active moderation record
		moderation := types.DisplayNameModeration{
			Member:       creatorStr,
			RejectedName: "TestName",
			Reason:       TestReportReason,
			ModeratedAt:  ctx.BlockHeight(),
			Active:       true,
		}
		k.DisplayNameModeration.Set(ctx, creatorStr, moderation)

		_, err := ms.AppealDisplayNameModeration(ctx, &types.MsgAppealDisplayNameModeration{
			Creator:      creatorStr,
			AppealReason: "My name is not inappropriate",
		})

		require.NoError(t, err)

		// Verify stake record exists
		expectedChallengeID := "dn_appeal:" + creatorStr + ":0"
		appealStake, err := k.DisplayNameAppealStake.Get(ctx, expectedChallengeID)
		require.NoError(t, err)
		require.Equal(t, creatorStr, appealStake.Appellant)
		require.Equal(t, expectedChallengeID, appealStake.ChallengeId)
	})

	t.Run("appeal just before period expires", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrMember2
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		params, _ := k.Params.Get(ctx)

		// Setup moderation at block 100
		moderation := types.DisplayNameModeration{
			Member:       creatorStr,
			RejectedName: "EdgeCaseName",
			Reason:       TestReportReason,
			ModeratedAt:  100,
			Active:       true,
		}
		k.DisplayNameModeration.Set(ctx, creatorStr, moderation)

		// Set block height to exactly the last valid block
		ctx = ctx.WithBlockHeight(100 + int64(params.DisplayNameAppealPeriodBlocks))

		_, err := ms.AppealDisplayNameModeration(ctx, &types.MsgAppealDisplayNameModeration{
			Creator:      creatorStr,
			AppealReason: TestAppealReason,
		})

		require.NoError(t, err)
	})

	t.Run("appeal one block after period expires fails", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrMember3
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		params, _ := k.Params.Get(ctx)

		// Setup moderation at block 100
		moderation := types.DisplayNameModeration{
			Member:       creatorStr,
			RejectedName: "ExpiredCaseName",
			Reason:       TestReportReason,
			ModeratedAt:  100,
			Active:       true,
		}
		k.DisplayNameModeration.Set(ctx, creatorStr, moderation)

		// Set block height to one block past the period
		ctx = ctx.WithBlockHeight(100 + int64(params.DisplayNameAppealPeriodBlocks) + 1)

		_, err := ms.AppealDisplayNameModeration(ctx, &types.MsgAppealDisplayNameModeration{
			Creator:      creatorStr,
			AppealReason: TestAppealReason,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrAppealPeriodExpired)
	})
}
