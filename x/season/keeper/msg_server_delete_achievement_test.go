package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerDeleteAchievement(t *testing.T) {
	t.Run("invalid authority address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.DeleteAchievement(f.ctx, &types.MsgDeleteAchievement{
			Authority:     "invalid-address",
			AchievementId: "ach1",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid authority address")
	})

	t.Run("not authorized", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		ms := keeper.NewMsgServerImpl(f.keeper)

		nonAuthority := TestAddrCreator
		nonAuthorityStr, _ := f.addressCodec.BytesToString(nonAuthority)

		_, err := ms.DeleteAchievement(ctx, &types.MsgDeleteAchievement{
			Authority:     nonAuthorityStr,
			AchievementId: "ach1",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotAuthorized)
	})

	t.Run("achievement not found", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		_, err := ms.DeleteAchievement(ctx, &types.MsgDeleteAchievement{
			Authority:     authority,
			AchievementId: "nonexistent",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrAchievementNotFound)
	})

	t.Run("successful deletion via governance", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Create achievement first
		existing := types.Achievement{
			AchievementId: "ach1",
			Name:          "To Be Deleted",
			XpReward:      50,
		}
		k.Achievement.Set(ctx, "ach1", existing)

		// Verify it exists
		_, err := k.Achievement.Get(ctx, "ach1")
		require.NoError(t, err)

		// Delete it
		_, err = ms.DeleteAchievement(ctx, &types.MsgDeleteAchievement{
			Authority:     authority,
			AchievementId: "ach1",
		})

		require.NoError(t, err)

		// Verify it's gone
		_, err = k.Achievement.Get(ctx, "ach1")
		require.Error(t, err)
	})

	t.Run("successful deletion via operations committee", func(t *testing.T) {
		committeeAddr := TestAddrMember1
		committeeAddrStr := committeeAddr.String()

		mockCommons := newMockCommonsKeeper(committeeAddrStr)
		f := initFixtureWithCommons(t, mockCommons)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		addrStr, _ := f.addressCodec.BytesToString(committeeAddr)

		// Create achievement first
		existing := types.Achievement{
			AchievementId: "ach1",
			Name:          "Committee Delete",
		}
		k.Achievement.Set(ctx, "ach1", existing)

		// Delete it
		_, err := ms.DeleteAchievement(ctx, &types.MsgDeleteAchievement{
			Authority:     addrStr,
			AchievementId: "ach1",
		})

		require.NoError(t, err)

		// Verify it's gone
		_, err = k.Achievement.Get(ctx, "ach1")
		require.Error(t, err)
	})

	t.Run("successful deletion via commons council policy", func(t *testing.T) {
		councilPolicyAddr := TestAddrCouncilPolicy
		mockCommons := newMockCommonsKeeperWithCouncil(councilPolicyAddr.String())
		f := initFixtureWithCommons(t, mockCommons)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		addrStr, _ := f.addressCodec.BytesToString(councilPolicyAddr)

		// Create achievement first
		existing := types.Achievement{
			AchievementId: "council_delete",
			Name:          "Council Delete",
		}
		k.Achievement.Set(ctx, "council_delete", existing)

		// Delete via council policy
		_, err := ms.DeleteAchievement(ctx, &types.MsgDeleteAchievement{
			Authority:     addrStr,
			AchievementId: "council_delete",
		})

		require.NoError(t, err)

		// Verify it's gone
		_, err = k.Achievement.Get(ctx, "council_delete")
		require.Error(t, err)
	})
}
