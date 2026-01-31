package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestMsgServerToggleTagBudget(t *testing.T) {
	f := initFixture(t)

	t.Run("invalid creator address", func(t *testing.T) {
		msg := &types.MsgToggleTagBudget{
			Creator:  "invalid",
			BudgetId: 1,
			Active:   false,
		}
		_, err := f.msgServer.ToggleTagBudget(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("budget not found", func(t *testing.T) {
		msg := &types.MsgToggleTagBudget{
			Creator:  testCreator,
			BudgetId: 999,
			Active:   false,
		}
		_, err := f.msgServer.ToggleTagBudget(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTagBudgetNotFound)
	})

	t.Run("not group account", func(t *testing.T) {
		budget := f.createTestTagBudget(t, testCreator, "test-tag", "1000000")

		msg := &types.MsgToggleTagBudget{
			Creator:  testCreator2,
			BudgetId: budget.Id,
			Active:   false,
		}
		_, err := f.msgServer.ToggleTagBudget(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotGroupAccount)
	})

	t.Run("pause budget", func(t *testing.T) {
		budget := f.createTestTagBudget(t, testCreator, "test-tag-2", "1000000")
		require.True(t, budget.Active)

		msg := &types.MsgToggleTagBudget{
			Creator:  testCreator,
			BudgetId: budget.Id,
			Active:   false,
		}
		_, err := f.msgServer.ToggleTagBudget(f.ctx, msg)
		require.NoError(t, err)

		// Verify budget is paused
		updatedBudget, err := f.keeper.TagBudget.Get(f.ctx, budget.Id)
		require.NoError(t, err)
		require.False(t, updatedBudget.Active)
	})

	t.Run("resume budget", func(t *testing.T) {
		budget := f.createTestTagBudget(t, testCreator, "test-tag-3", "1000000")

		// First pause it
		budget.Active = false
		f.keeper.TagBudget.Set(f.ctx, budget.Id, budget)

		msg := &types.MsgToggleTagBudget{
			Creator:  testCreator,
			BudgetId: budget.Id,
			Active:   true,
		}
		_, err := f.msgServer.ToggleTagBudget(f.ctx, msg)
		require.NoError(t, err)

		// Verify budget is active
		updatedBudget, err := f.keeper.TagBudget.Get(f.ctx, budget.Id)
		require.NoError(t, err)
		require.True(t, updatedBudget.Active)
	})
}
