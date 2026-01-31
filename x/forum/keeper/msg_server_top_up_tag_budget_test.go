package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestMsgServerTopUpTagBudget(t *testing.T) {
	f := initFixture(t)

	t.Run("invalid creator address", func(t *testing.T) {
		msg := &types.MsgTopUpTagBudget{
			Creator:  "invalid",
			BudgetId: 1,
			Amount:   "100000",
		}
		_, err := f.msgServer.TopUpTagBudget(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("budget not found", func(t *testing.T) {
		msg := &types.MsgTopUpTagBudget{
			Creator:  testCreator,
			BudgetId: 999,
			Amount:   "100000",
		}
		_, err := f.msgServer.TopUpTagBudget(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTagBudgetNotFound)
	})

	t.Run("any member can top up", func(t *testing.T) {
		budget := f.createTestTagBudget(t, testCreator, "test-tag", "1000000")

		// Note: IsGroupMember is a stub that returns true for all users
		// In production, only group members could top up
		msg := &types.MsgTopUpTagBudget{
			Creator:  testCreator2,
			BudgetId: budget.Id,
			Amount:   "100000",
		}
		_, err := f.msgServer.TopUpTagBudget(f.ctx, msg)
		require.NoError(t, err)
	})

	t.Run("invalid amount", func(t *testing.T) {
		budget := f.createTestTagBudget(t, testCreator, "test-tag-2", "1000000")

		msg := &types.MsgTopUpTagBudget{
			Creator:  testCreator,
			BudgetId: budget.Id,
			Amount:   "invalid",
		}
		_, err := f.msgServer.TopUpTagBudget(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInvalidAmount)
	})

	t.Run("successful top up", func(t *testing.T) {
		budget := f.createTestTagBudget(t, testCreator, "test-tag-3", "1000000")

		msg := &types.MsgTopUpTagBudget{
			Creator:  testCreator,
			BudgetId: budget.Id,
			Amount:   "500000",
		}
		_, err := f.msgServer.TopUpTagBudget(f.ctx, msg)
		require.NoError(t, err)

		// Verify budget balance increased
		updatedBudget, err := f.keeper.TagBudget.Get(f.ctx, budget.Id)
		require.NoError(t, err)
		require.Equal(t, "1500000", updatedBudget.PoolBalance)
	})
}
