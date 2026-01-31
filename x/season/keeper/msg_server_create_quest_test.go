package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerCreateQuest(t *testing.T) {
	t.Run("invalid authority address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.CreateQuest(f.ctx, &types.MsgCreateQuest{
			Authority: "invalid-address",
			QuestId:   "quest1",
			Name:      "Test Quest",
			XpReward:  50,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid authority address")
	})

	t.Run("not operations committee", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		ms := keeper.NewMsgServerImpl(f.keeper)

		nonAuthority := TestAddrCreator
		nonAuthorityStr, _ := f.addressCodec.BytesToString(nonAuthority)

		_, err := ms.CreateQuest(ctx, &types.MsgCreateQuest{
			Authority: nonAuthorityStr,
			QuestId:   "quest1",
			Name:      "Test Quest",
			XpReward:  50,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotOperationsCommittee)
	})

	t.Run("empty quest id", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		ms := keeper.NewMsgServerImpl(f.keeper)

		authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

		_, err := ms.CreateQuest(ctx, &types.MsgCreateQuest{
			Authority: authority,
			QuestId:   "", // Empty ID
			Name:      "Test Quest",
			XpReward:  50,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "quest ID cannot be empty")
	})

	t.Run("quest id already exists", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Create existing quest
		existingQuest := types.Quest{
			QuestId:  "existing_quest",
			Name:     "Existing Quest",
			XpReward: 50,
			Active:   true,
		}
		k.Quest.Set(ctx, "existing_quest", existingQuest)

		_, err := ms.CreateQuest(ctx, &types.MsgCreateQuest{
			Authority: authority,
			QuestId:   "existing_quest",
			Name:      "New Quest",
			XpReward:  50,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrQuestIdAlreadyExists)
	})

	t.Run("xp reward too high", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())
		params, _ := k.Params.Get(ctx)

		_, err := ms.CreateQuest(ctx, &types.MsgCreateQuest{
			Authority: authority,
			QuestId:   "high_xp_quest",
			Name:      "High XP Quest",
			XpReward:  params.MaxQuestXpReward + 1, // Exceeds max
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrQuestXpRewardTooHigh)
	})

	t.Run("successful quest creation", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		_, err := ms.CreateQuest(ctx, &types.MsgCreateQuest{
			Authority:   authority,
			QuestId:     "new_quest",
			Name:        "New Quest",
			Description: "A new test quest",
			XpReward:    50,
			Repeatable:  true,
		})

		require.NoError(t, err)

		// Verify quest was created
		quest, err := k.Quest.Get(ctx, "new_quest")
		require.NoError(t, err)
		require.Equal(t, "new_quest", quest.QuestId)
		require.Equal(t, "New Quest", quest.Name)
		require.Equal(t, "A new test quest", quest.Description)
		require.Equal(t, uint64(50), quest.XpReward)
		require.True(t, quest.Repeatable)
		require.True(t, quest.Active)
	})

	t.Run("successful quest with all options", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		_, err := ms.CreateQuest(ctx, &types.MsgCreateQuest{
			Authority:           authority,
			QuestId:             "full_quest",
			Name:                "Full Quest",
			Description:         "A quest with all options",
			XpReward:            75,
			Repeatable:          true,
			CooldownEpochs:      10,
			Season:              1,
			StartBlock:          100,
			EndBlock:            1000,
			MinLevel:            5,
			RequiredAchievement: "first_steps",
			PrerequisiteQuest:   "intro_quest",
			QuestChain:          "main_story",
		})

		require.NoError(t, err)

		quest, err := k.Quest.Get(ctx, "full_quest")
		require.NoError(t, err)
		require.Equal(t, uint64(10), quest.CooldownEpochs)
		require.Equal(t, uint64(1), quest.Season)
		require.Equal(t, int64(100), quest.StartBlock)
		require.Equal(t, int64(1000), quest.EndBlock)
		require.Equal(t, uint64(5), quest.MinLevel)
		require.Equal(t, "first_steps", quest.RequiredAchievement)
		require.Equal(t, "intro_quest", quest.PrerequisiteQuest)
		require.Equal(t, "main_story", quest.QuestChain)
	})
}
