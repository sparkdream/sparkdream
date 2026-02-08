package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerUpdateAchievement(t *testing.T) {
	t.Run("invalid authority address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.UpdateAchievement(f.ctx, &types.MsgUpdateAchievement{
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

		_, err := ms.UpdateAchievement(ctx, &types.MsgUpdateAchievement{
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

		_, err := ms.UpdateAchievement(ctx, &types.MsgUpdateAchievement{
			Authority:     authority,
			AchievementId: "nonexistent",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrAchievementNotFound)
	})

	t.Run("successful update via governance", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Create achievement first
		existing := types.Achievement{
			AchievementId:        "ach1",
			Name:                 "Original Name",
			Description:          "Original Description",
			Rarity:               types.Rarity_RARITY_COMMON,
			XpReward:             50,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_VOTES_CAST,
			RequirementThreshold: 5,
		}
		k.Achievement.Set(ctx, "ach1", existing)

		// Update it
		_, err := ms.UpdateAchievement(ctx, &types.MsgUpdateAchievement{
			Authority:            authority,
			AchievementId:        "ach1",
			Name:                 "Updated Name",
			Description:          "Updated Description",
			Rarity:               uint32(types.Rarity_RARITY_RARE),
			XpReward:             100,
			RequirementType:      uint32(types.RequirementType_REQUIREMENT_TYPE_INITIATIVES_COMPLETED),
			RequirementThreshold: 10,
		})

		require.NoError(t, err)

		// Verify updates
		ach, err := k.Achievement.Get(ctx, "ach1")
		require.NoError(t, err)
		require.Equal(t, "Updated Name", ach.Name)
		require.Equal(t, "Updated Description", ach.Description)
		require.Equal(t, types.Rarity_RARITY_RARE, ach.Rarity)
		require.Equal(t, uint64(100), ach.XpReward)
		require.Equal(t, types.RequirementType_REQUIREMENT_TYPE_INITIATIVES_COMPLETED, ach.RequirementType)
		require.Equal(t, uint64(10), ach.RequirementThreshold)
	})

	t.Run("successful update via operations committee", func(t *testing.T) {
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
			Name:          "Original",
			XpReward:      50,
		}
		k.Achievement.Set(ctx, "ach1", existing)

		// Update it
		_, err := ms.UpdateAchievement(ctx, &types.MsgUpdateAchievement{
			Authority:     addrStr,
			AchievementId: "ach1",
			Name:          "Committee Updated",
			XpReward:      75,
		})

		require.NoError(t, err)

		// Verify
		ach, err := k.Achievement.Get(ctx, "ach1")
		require.NoError(t, err)
		require.Equal(t, "Committee Updated", ach.Name)
		require.Equal(t, uint64(75), ach.XpReward)
	})

	t.Run("partial update preserves unset fields", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Create achievement
		existing := types.Achievement{
			AchievementId:        "ach1",
			Name:                 "Original Name",
			Description:          "Original Description",
			Rarity:               types.Rarity_RARITY_EPIC,
			XpReward:             200,
			RequirementType:      types.RequirementType_REQUIREMENT_TYPE_JURY_DUTY,
			RequirementThreshold: 3,
		}
		k.Achievement.Set(ctx, "ach1", existing)

		// Update only name (description empty means preserve)
		_, err := ms.UpdateAchievement(ctx, &types.MsgUpdateAchievement{
			Authority:     authority,
			AchievementId: "ach1",
			Name:          "New Name Only",
			// Other fields are zero/empty
		})

		require.NoError(t, err)

		// Verify name changed but description preserved
		ach, err := k.Achievement.Get(ctx, "ach1")
		require.NoError(t, err)
		require.Equal(t, "New Name Only", ach.Name)
		require.Equal(t, "Original Description", ach.Description) // Preserved
		// Note: XpReward and threshold become 0 because they're always updated
		require.Equal(t, uint64(0), ach.XpReward)
	})
}
