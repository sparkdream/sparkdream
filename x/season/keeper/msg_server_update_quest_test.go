package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerUpdateQuest(t *testing.T) {
	t.Run("invalid authority address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.UpdateQuest(f.ctx, &types.MsgUpdateQuest{
			Authority: "invalid-address",
			QuestId:   "quest1",
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

		_, err := ms.UpdateQuest(ctx, &types.MsgUpdateQuest{
			Authority: nonAuthorityStr,
			QuestId:   "quest1",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotAuthorized)
	})

	t.Run("quest not found", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		_, err := ms.UpdateQuest(ctx, &types.MsgUpdateQuest{
			Authority: authority,
			QuestId:   "nonexistent",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrQuestNotFound)
	})

	t.Run("xp reward too high", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())
		params, _ := k.Params.Get(ctx)

		// Create quest first
		existing := types.Quest{
			QuestId:  "quest1",
			Name:     "Test Quest",
			XpReward: 50,
			Active:   true,
		}
		k.Quest.Set(ctx, "quest1", existing)

		_, err := ms.UpdateQuest(ctx, &types.MsgUpdateQuest{
			Authority: authority,
			QuestId:   "quest1",
			XpReward:  params.MaxQuestXpReward + 1,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrQuestXpRewardTooHigh)
	})

	t.Run("successful update via governance", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Create quest first
		existing := types.Quest{
			QuestId:        "quest1",
			Name:           "Original Quest",
			Description:    "Original Description",
			XpReward:       50,
			Repeatable:     false,
			CooldownEpochs: 0,
			Active:         true,
		}
		k.Quest.Set(ctx, "quest1", existing)

		// Update it
		_, err := ms.UpdateQuest(ctx, &types.MsgUpdateQuest{
			Authority:      authority,
			QuestId:        "quest1",
			Name:           "Updated Quest",
			Description:    "Updated Description",
			XpReward:       100,
			Repeatable:     true,
			CooldownEpochs: 5,
			Active:         true,
		})

		require.NoError(t, err)

		// Verify updates
		quest, err := k.Quest.Get(ctx, "quest1")
		require.NoError(t, err)
		require.Equal(t, "Updated Quest", quest.Name)
		require.Equal(t, "Updated Description", quest.Description)
		require.Equal(t, uint64(100), quest.XpReward)
		require.True(t, quest.Repeatable)
		require.Equal(t, uint64(5), quest.CooldownEpochs)
		require.True(t, quest.Active)
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

		// Create quest first
		existing := types.Quest{
			QuestId:  "quest1",
			Name:     "Original",
			XpReward: 50,
			Active:   true,
		}
		k.Quest.Set(ctx, "quest1", existing)

		// Update it
		_, err := ms.UpdateQuest(ctx, &types.MsgUpdateQuest{
			Authority: addrStr,
			QuestId:   "quest1",
			Name:      "Committee Updated",
			XpReward:  75,
			Active:    true,
		})

		require.NoError(t, err)

		// Verify
		quest, err := k.Quest.Get(ctx, "quest1")
		require.NoError(t, err)
		require.Equal(t, "Committee Updated", quest.Name)
		require.Equal(t, uint64(75), quest.XpReward)
	})

	t.Run("deactivate quest via update", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Create active quest
		existing := types.Quest{
			QuestId: "quest1",
			Name:    "Active Quest",
			Active:  true,
		}
		k.Quest.Set(ctx, "quest1", existing)

		// Deactivate via update
		_, err := ms.UpdateQuest(ctx, &types.MsgUpdateQuest{
			Authority: authority,
			QuestId:   "quest1",
			Name:      "Now Inactive",
			Active:    false,
		})

		require.NoError(t, err)

		quest, err := k.Quest.Get(ctx, "quest1")
		require.NoError(t, err)
		require.False(t, quest.Active)
	})

	t.Run("reactivate quest via update", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Create inactive quest
		existing := types.Quest{
			QuestId: "quest1",
			Name:    "Inactive Quest",
			Active:  false,
		}
		k.Quest.Set(ctx, "quest1", existing)

		// Reactivate via update
		_, err := ms.UpdateQuest(ctx, &types.MsgUpdateQuest{
			Authority: authority,
			QuestId:   "quest1",
			Name:      "Now Active",
			Active:    true,
		})

		require.NoError(t, err)

		quest, err := k.Quest.Get(ctx, "quest1")
		require.NoError(t, err)
		require.True(t, quest.Active)
	})

	t.Run("update all quest options", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Create basic quest
		existing := types.Quest{
			QuestId: "quest1",
			Name:    "Basic Quest",
			Active:  true,
		}
		k.Quest.Set(ctx, "quest1", existing)

		// Update with all options (XP must be within MaxQuestXpReward limit of 100)
		_, err := ms.UpdateQuest(ctx, &types.MsgUpdateQuest{
			Authority:           authority,
			QuestId:             "quest1",
			Name:                "Full Quest",
			Description:         "A quest with all options",
			XpReward:            100,
			Repeatable:          true,
			CooldownEpochs:      10,
			Season:              2,
			StartBlock:          100,
			EndBlock:            10000,
			MinLevel:            5,
			RequiredAchievement: "first_steps",
			PrerequisiteQuest:   "intro",
			QuestChain:          "main_story",
			Active:              true,
		})

		require.NoError(t, err)

		quest, err := k.Quest.Get(ctx, "quest1")
		require.NoError(t, err)
		require.Equal(t, "Full Quest", quest.Name)
		require.Equal(t, "A quest with all options", quest.Description)
		require.Equal(t, uint64(100), quest.XpReward)
		require.True(t, quest.Repeatable)
		require.Equal(t, uint64(10), quest.CooldownEpochs)
		require.Equal(t, uint64(2), quest.Season)
		require.Equal(t, int64(100), quest.StartBlock)
		require.Equal(t, int64(10000), quest.EndBlock)
		require.Equal(t, uint64(5), quest.MinLevel)
		require.Equal(t, "first_steps", quest.RequiredAchievement)
		require.Equal(t, "intro", quest.PrerequisiteQuest)
		require.Equal(t, "main_story", quest.QuestChain)
	})
}
