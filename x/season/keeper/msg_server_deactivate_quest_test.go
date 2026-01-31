package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func TestMsgServerDeactivateQuest(t *testing.T) {
	t.Run("invalid authority address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.DeactivateQuest(f.ctx, &types.MsgDeactivateQuest{
			Authority: "invalid-address",
			QuestId:   "quest1",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid authority address")
	})

	t.Run("not operations committee", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		nonAuthority := TestAddrCreator
		nonAuthorityStr, _ := f.addressCodec.BytesToString(nonAuthority)

		SetupQuest(t, k, ctx, "quest1", 50, true)

		_, err := ms.DeactivateQuest(ctx, &types.MsgDeactivateQuest{
			Authority: nonAuthorityStr,
			QuestId:   "quest1",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotOperationsCommittee)
	})

	t.Run("quest not found", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		// Use the module authority which passes IsOperationsCommittee
		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		_, err := ms.DeactivateQuest(ctx, &types.MsgDeactivateQuest{
			Authority: authority,
			QuestId:   "nonexistent_quest",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrQuestNotFound)
	})

	t.Run("quest already inactive", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		// Create an inactive quest
		SetupQuest(t, k, ctx, "inactive_quest", 50, false) // active = false

		_, err := ms.DeactivateQuest(ctx, &types.MsgDeactivateQuest{
			Authority: authority,
			QuestId:   "inactive_quest",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrQuestNotActive)
	})

	t.Run("successful deactivation", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		SetupQuest(t, k, ctx, "active_quest", 100, true)

		// Verify quest is active
		quest, _ := k.Quest.Get(ctx, "active_quest")
		require.True(t, quest.Active)

		_, err := ms.DeactivateQuest(ctx, &types.MsgDeactivateQuest{
			Authority: authority,
			QuestId:   "active_quest",
		})

		require.NoError(t, err)

		// Verify quest is now inactive
		quest, _ = k.Quest.Get(ctx, "active_quest")
		require.False(t, quest.Active)
	})

	t.Run("deactivate multiple quests", func(t *testing.T) {
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx)
		k := f.keeper
		ms := keeper.NewMsgServerImpl(k)

		authority, _ := f.addressCodec.BytesToString(k.GetAuthority())

		SetupQuest(t, k, ctx, "quest_a", 25, true)
		SetupQuest(t, k, ctx, "quest_b", 50, true)
		SetupQuest(t, k, ctx, "quest_c", 75, true)

		// Deactivate quest_a
		_, err := ms.DeactivateQuest(ctx, &types.MsgDeactivateQuest{
			Authority: authority,
			QuestId:   "quest_a",
		})
		require.NoError(t, err)

		// Verify only quest_a is inactive
		questA, _ := k.Quest.Get(ctx, "quest_a")
		questB, _ := k.Quest.Get(ctx, "quest_b")
		questC, _ := k.Quest.Get(ctx, "quest_c")
		require.False(t, questA.Active)
		require.True(t, questB.Active)
		require.True(t, questC.Active)

		// Deactivate quest_c
		_, err = ms.DeactivateQuest(ctx, &types.MsgDeactivateQuest{
			Authority: authority,
			QuestId:   "quest_c",
		})
		require.NoError(t, err)

		questC, _ = k.Quest.Get(ctx, "quest_c")
		require.False(t, questC.Active)
	})
}
