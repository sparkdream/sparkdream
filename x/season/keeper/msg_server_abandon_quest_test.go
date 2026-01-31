package keeper_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerAbandonQuest(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.AbandonQuest(f.ctx, &types.MsgAbandonQuest{
			Creator: "invalid-address",
			QuestId: "quest1",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("quest not started", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		SetupBasicMemberProfile(t, k, ctx, creator)
		SetupQuest(t, k, ctx, "quest1", 50, true)

		_, err := ms.AbandonQuest(ctx, &types.MsgAbandonQuest{
			Creator: creatorStr,
			QuestId: "quest1",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrQuestNotStarted)
	})

	t.Run("cannot abandon completed quest", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		SetupBasicMemberProfile(t, k, ctx, creator)
		SetupQuest(t, k, ctx, "completed_quest", 50, true)

		// Create completed progress
		key := fmt.Sprintf("%s:%s", creatorStr, "completed_quest")
		progress := types.MemberQuestProgress{
			MemberQuest:    key,
			Completed:      true,
			CompletedBlock: 100,
		}
		k.MemberQuestProgress.Set(ctx, key, progress)

		_, err := ms.AbandonQuest(ctx, &types.MsgAbandonQuest{
			Creator: creatorStr,
			QuestId: "completed_quest",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "quest already completed")
	})

	t.Run("successful abandon non-repeatable quest", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrMember1
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		SetupBasicMemberProfile(t, k, ctx, creator)

		// Create non-repeatable quest
		quest := types.Quest{
			QuestId:    "non_repeatable",
			Name:       "Non-Repeatable Quest",
			XpReward:   50,
			Active:     true,
			Repeatable: false,
		}
		k.Quest.Set(ctx, "non_repeatable", quest)

		// Create in-progress
		key := fmt.Sprintf("%s:%s", creatorStr, "non_repeatable")
		progress := types.MemberQuestProgress{
			MemberQuest:       key,
			ObjectiveProgress: []uint64{5},
			Completed:         false,
		}
		k.MemberQuestProgress.Set(ctx, key, progress)

		_, err := ms.AbandonQuest(ctx, &types.MsgAbandonQuest{
			Creator: creatorStr,
			QuestId: "non_repeatable",
		})

		require.NoError(t, err)

		// Verify progress was removed
		_, err = k.MemberQuestProgress.Get(ctx, key)
		require.Error(t, err)
	})

	t.Run("successful abandon repeatable quest with cooldown", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrMember2
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		SetupBasicMemberProfile(t, k, ctx, creator)

		// Create repeatable quest with cooldown
		quest := types.Quest{
			QuestId:        "repeatable_cooldown",
			Name:           "Repeatable Quest",
			XpReward:       50,
			Active:         true,
			Repeatable:     true,
			CooldownEpochs: 5,
			Objectives:     []*types.QuestObjective{{Description: "Objective 1", Target: 10}},
		}
		k.Quest.Set(ctx, "repeatable_cooldown", quest)

		// Create in-progress
		key := fmt.Sprintf("%s:%s", creatorStr, "repeatable_cooldown")
		progress := types.MemberQuestProgress{
			MemberQuest:       key,
			ObjectiveProgress: []uint64{3}, // Partial progress
			Completed:         false,
		}
		k.MemberQuestProgress.Set(ctx, key, progress)

		_, err := ms.AbandonQuest(ctx, &types.MsgAbandonQuest{
			Creator: creatorStr,
			QuestId: "repeatable_cooldown",
		})

		require.NoError(t, err)

		// Verify progress was marked complete (to start cooldown)
		updatedProgress, err := k.MemberQuestProgress.Get(ctx, key)
		require.NoError(t, err)
		require.True(t, updatedProgress.Completed)
		require.Equal(t, ctx.BlockHeight(), updatedProgress.CompletedBlock)
		// Progress should be reset
		require.Len(t, updatedProgress.ObjectiveProgress, 1)
		require.Equal(t, uint64(0), updatedProgress.ObjectiveProgress[0])
	})

	t.Run("successful abandon repeatable quest without cooldown", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrMember3
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		SetupBasicMemberProfile(t, k, ctx, creator)

		// Create repeatable quest without cooldown
		quest := types.Quest{
			QuestId:        "repeatable_no_cooldown",
			Name:           "Repeatable No Cooldown",
			XpReward:       50,
			Active:         true,
			Repeatable:     true,
			CooldownEpochs: 0, // No cooldown
		}
		k.Quest.Set(ctx, "repeatable_no_cooldown", quest)

		// Create in-progress
		key := fmt.Sprintf("%s:%s", creatorStr, "repeatable_no_cooldown")
		progress := types.MemberQuestProgress{
			MemberQuest:       key,
			ObjectiveProgress: []uint64{},
			Completed:         false,
		}
		k.MemberQuestProgress.Set(ctx, key, progress)

		_, err := ms.AbandonQuest(ctx, &types.MsgAbandonQuest{
			Creator: creatorStr,
			QuestId: "repeatable_no_cooldown",
		})

		require.NoError(t, err)

		// Verify progress was removed (no cooldown = just delete)
		_, err = k.MemberQuestProgress.Get(ctx, key)
		require.Error(t, err)
	})
}
