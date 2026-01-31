package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryTagBudgets(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.TagBudgets(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no budgets", func(t *testing.T) {
		resp, err := qs.TagBudgets(f.ctx, &types.QueryTagBudgetsRequest{})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.BudgetId)
	})

	t.Run("has budgets", func(t *testing.T) {
		budget := f.createTestTagBudget(t, testCreator, "budgets-list-tag", "1000000")

		resp, err := qs.TagBudgets(f.ctx, &types.QueryTagBudgetsRequest{})
		require.NoError(t, err)
		require.NotZero(t, resp.BudgetId)
		_ = budget
	})
}
