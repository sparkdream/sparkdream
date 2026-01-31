package keeper_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerClaimQuestReward(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.ClaimQuestReward(f.ctx, &types.MsgClaimQuestReward{
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

		// No progress record exists

		_, err := ms.ClaimQuestReward(ctx, &types.MsgClaimQuestReward{
			Creator: creatorStr,
			QuestId: "quest1",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrQuestNotStarted)
	})

	t.Run("quest already claimed", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		SetupBasicMemberProfile(t, k, ctx, creator)
		SetupQuest(t, k, ctx, "claimed_quest", 50, true)

		// Create already completed progress
		key := fmt.Sprintf("%s:%s", creatorStr, "claimed_quest")
		progress := types.MemberQuestProgress{
			MemberQuest:    key,
			Completed:      true,
			CompletedBlock: 100,
		}
		k.MemberQuestProgress.Set(ctx, key, progress)

		_, err := ms.ClaimQuestReward(ctx, &types.MsgClaimQuestReward{
			Creator: creatorStr,
			QuestId: "claimed_quest",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrQuestAlreadyClaimed)
	})

	t.Run("quest not complete", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrCreator
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		SetupBasicMemberProfile(t, k, ctx, creator)

		// Create quest with objectives
		objectives := []*types.QuestObjective{
			{Description: "Objective 1", Target: 10},
		}
		SetupQuestWithObjectives(t, k, ctx, "incomplete_quest", 50, objectives)

		// Create progress with incomplete objectives
		key := fmt.Sprintf("%s:%s", creatorStr, "incomplete_quest")
		progress := types.MemberQuestProgress{
			MemberQuest:       key,
			ObjectiveProgress: []uint64{5}, // Only 5/10 complete
			Completed:         false,
		}
		k.MemberQuestProgress.Set(ctx, key, progress)

		_, err := ms.ClaimQuestReward(ctx, &types.MsgClaimQuestReward{
			Creator: creatorStr,
			QuestId: "incomplete_quest",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrQuestNotComplete)
	})

	t.Run("successful claim with xp reward", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrMember1
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		// Advance to block 100 so CompletedBlock is non-zero
		ctx = ctx.WithBlockHeight(100)

		SetupBasicMemberProfile(t, k, ctx, creator)

		// Create quest with objectives
		objectives := []*types.QuestObjective{
			{Description: "Objective 1", Target: 10},
			{Description: "Objective 2", Target: 5},
		}
		SetupQuestWithObjectives(t, k, ctx, "complete_quest", 75, objectives)

		// Create progress with complete objectives
		key := fmt.Sprintf("%s:%s", creatorStr, "complete_quest")
		progress := types.MemberQuestProgress{
			MemberQuest:       key,
			ObjectiveProgress: []uint64{10, 5}, // All complete
			Completed:         false,
		}
		k.MemberQuestProgress.Set(ctx, key, progress)

		// Get initial XP
		profileBefore, _ := k.MemberProfile.Get(ctx, creatorStr)
		initialXp := profileBefore.SeasonXp

		_, err := ms.ClaimQuestReward(ctx, &types.MsgClaimQuestReward{
			Creator: creatorStr,
			QuestId: "complete_quest",
		})

		require.NoError(t, err)

		// Verify progress marked complete
		updatedProgress, err := k.MemberQuestProgress.Get(ctx, key)
		require.NoError(t, err)
		require.True(t, updatedProgress.Completed)
		require.Equal(t, int64(100), updatedProgress.CompletedBlock)

		// Verify XP was granted
		profileAfter, err := k.MemberProfile.Get(ctx, creatorStr)
		require.NoError(t, err)
		require.Equal(t, initialXp+75, profileAfter.SeasonXp)
		require.Equal(t, initialXp+75, profileAfter.LifetimeXp)
	})

	t.Run("quest not found after starting", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		creator := TestAddrMember2
		creatorStr, _ := f.addressCodec.BytesToString(creator)

		SetupBasicMemberProfile(t, k, ctx, creator)

		// Create progress without quest (edge case - quest deleted after starting)
		key := fmt.Sprintf("%s:%s", creatorStr, "deleted_quest")
		progress := types.MemberQuestProgress{
			MemberQuest:       key,
			ObjectiveProgress: []uint64{},
			Completed:         false,
		}
		k.MemberQuestProgress.Set(ctx, key, progress)

		_, err := ms.ClaimQuestReward(ctx, &types.MsgClaimQuestReward{
			Creator: creatorStr,
			QuestId: "deleted_quest",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrQuestNotFound)
	})
}
