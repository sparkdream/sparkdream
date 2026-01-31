package keeper_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SetupQuest creates a quest for testing
func SetupQuest(t *testing.T, k keeper.Keeper, ctx sdk.Context, questId string, xpReward uint64, active bool) {
	t.Helper()
	quest := types.Quest{
		QuestId:    questId,
		Name:       "Test Quest " + questId,
		XpReward:   xpReward,
		Active:     active,
		Objectives: []*types.QuestObjective{},
	}
	err := k.Quest.Set(ctx, questId, quest)
	require.NoError(t, err)
}

// SetupQuestWithObjectives creates a quest with objectives
func SetupQuestWithObjectives(t *testing.T, k keeper.Keeper, ctx sdk.Context, questId string, xpReward uint64, objectives []*types.QuestObjective) {
	t.Helper()
	quest := types.Quest{
		QuestId:    questId,
		Name:       "Test Quest " + questId,
		XpReward:   xpReward,
		Active:     true,
		Objectives: objectives,
	}
	err := k.Quest.Set(ctx, questId, quest)
	require.NoError(t, err)
}

// CreateObjective is a helper to create a QuestObjective
func CreateObjective(description string, target uint64) *types.QuestObjective {
	return &types.QuestObjective{
		Description: description,
		Target:      target,
	}
}

func TestMsgServerStartQuest(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.StartQuest(f.ctx, &types.MsgStartQuest{
			Creator: "invalid-address",
			QuestId: "quest1",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("member profile not found", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		// Don't setup profile
		SetupQuest(t, k, ctx, "quest1", 50, true)

		_, err := ms.StartQuest(ctx, &types.MsgStartQuest{
			Creator: creatorStr,
			QuestId: "quest1",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "profile not found")
	})

	t.Run("quest not found", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		SetupBasicMemberProfile(t, k, ctx, creator)

		_, err := ms.StartQuest(ctx, &types.MsgStartQuest{
			Creator: creatorStr,
			QuestId: "nonexistent",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrQuestNotFound)
	})

	t.Run("quest not active", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		SetupBasicMemberProfile(t, k, ctx, creator)
		SetupQuest(t, k, ctx, "inactive_quest", 50, false) // Inactive

		_, err := ms.StartQuest(ctx, &types.MsgStartQuest{
			Creator: creatorStr,
			QuestId: "inactive_quest",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrQuestNotActive)
	})

	t.Run("quest already started", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		SetupBasicMemberProfile(t, k, ctx, creator)
		SetupQuest(t, k, ctx, "started_quest", 50, true)

		// Create existing progress
		key := fmt.Sprintf("%s:%s", creatorStr, "started_quest")
		progress := types.MemberQuestProgress{
			MemberQuest:       key,
			ObjectiveProgress: []uint64{},
			Completed:         false,
		}
		k.MemberQuestProgress.Set(ctx, key, progress)

		_, err := ms.StartQuest(ctx, &types.MsgStartQuest{
			Creator: creatorStr,
			QuestId: "started_quest",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrQuestAlreadyStarted)
	})

	t.Run("level requirement not met", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		// Setup profile with level 1
		SetupBasicMemberProfile(t, k, ctx, creator)

		// Create quest requiring level 5
		quest := types.Quest{
			QuestId:  "high_level_quest",
			Name:     "High Level Quest",
			XpReward: 50,
			Active:   true,
			MinLevel: 5,
		}
		k.Quest.Set(ctx, "high_level_quest", quest)

		_, err := ms.StartQuest(ctx, &types.MsgStartQuest{
			Creator: creatorStr,
			QuestId: "high_level_quest",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrQuestLevelTooLow)
	})

	t.Run("successful quest start", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrMember1
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		SetupBasicMemberProfile(t, k, ctx, creator)
		objectives := []*types.QuestObjective{
			{Description: "Objective 1", Target: 10},
			{Description: "Objective 2", Target: 5},
		}
		SetupQuestWithObjectives(t, k, ctx, "new_quest", 50, objectives)

		_, err := ms.StartQuest(ctx, &types.MsgStartQuest{
			Creator: creatorStr,
			QuestId: "new_quest",
		})

		require.NoError(t, err)

		// Verify progress was created
		key := fmt.Sprintf("%s:%s", creatorStr, "new_quest")
		progress, err := k.MemberQuestProgress.Get(ctx, key)
		require.NoError(t, err)
		require.False(t, progress.Completed)
		require.Len(t, progress.ObjectiveProgress, 2)
		require.Equal(t, uint64(0), progress.ObjectiveProgress[0])
		require.Equal(t, uint64(0), progress.ObjectiveProgress[1])
	})

	t.Run("non-repeatable quest already completed", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrMember2
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		SetupBasicMemberProfile(t, k, ctx, creator)

		// Create non-repeatable quest
		quest := types.Quest{
			QuestId:    "one_time_quest",
			Name:       "One Time Quest",
			XpReward:   50,
			Active:     true,
			Repeatable: false,
		}
		k.Quest.Set(ctx, "one_time_quest", quest)

		// Create completed progress
		key := fmt.Sprintf("%s:%s", creatorStr, "one_time_quest")
		progress := types.MemberQuestProgress{
			MemberQuest:    key,
			Completed:      true,
			CompletedBlock: 100,
		}
		k.MemberQuestProgress.Set(ctx, key, progress)

		_, err := ms.StartQuest(ctx, &types.MsgStartQuest{
			Creator: creatorStr,
			QuestId: "one_time_quest",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrQuestAlreadyClaimed)
	})
}
