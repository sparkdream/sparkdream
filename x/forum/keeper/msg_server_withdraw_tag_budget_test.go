package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestMsgServerWithdrawTagBudget(t *testing.T) {
	f := initFixture(t)

	t.Run("invalid creator address", func(t *testing.T) {
		msg := &types.MsgWithdrawTagBudget{
			Creator:  "invalid",
			BudgetId: 1,
		}
		_, err := f.msgServer.WithdrawTagBudget(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("budget not found", func(t *testing.T) {
		msg := &types.MsgWithdrawTagBudget{
			Creator:  testCreator,
			BudgetId: 999,
		}
		_, err := f.msgServer.WithdrawTagBudget(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTagBudgetNotFound)
	})

	t.Run("not group account", func(t *testing.T) {
		budget := f.createTestTagBudget(t, testCreator, "withdraw-tag-1", "1000000")

		msg := &types.MsgWithdrawTagBudget{
			Creator:  testCreator2,
			BudgetId: budget.Id,
		}
		_, err := f.msgServer.WithdrawTagBudget(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotGroupAccount)
	})

	t.Run("empty budget", func(t *testing.T) {
		budget := f.createTestTagBudget(t, testCreator, "withdraw-tag-2", "0")

		msg := &types.MsgWithdrawTagBudget{
			Creator:  testCreator,
			BudgetId: budget.Id,
		}
		_, err := f.msgServer.WithdrawTagBudget(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTagBudgetInsufficient)
	})

	t.Run("successful withdraw", func(t *testing.T) {
		budget := f.createTestTagBudget(t, testCreator, "withdraw-tag-3", "1000000")

		msg := &types.MsgWithdrawTagBudget{
			Creator:  testCreator,
			BudgetId: budget.Id,
		}
		_, err := f.msgServer.WithdrawTagBudget(f.ctx, msg)
		require.NoError(t, err)

		// Verify budget is zeroed and deactivated
		updatedBudget, err := f.keeper.TagBudget.Get(f.ctx, budget.Id)
		require.NoError(t, err)
		require.Equal(t, "0", updatedBudget.PoolBalance)
		require.False(t, updatedBudget.Active)
	})
}
