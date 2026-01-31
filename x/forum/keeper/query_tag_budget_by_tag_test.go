package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryTagBudgetByTag(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.TagBudgetByTag(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty tag", func(t *testing.T) {
		_, err := qs.TagBudgetByTag(f.ctx, &types.QueryTagBudgetByTagRequest{Tag: ""})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no budget for tag", func(t *testing.T) {
		resp, err := qs.TagBudgetByTag(f.ctx, &types.QueryTagBudgetByTagRequest{Tag: "nonexistent"})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.BudgetId)
		require.Empty(t, resp.PoolBalance)
	})

	t.Run("has budget", func(t *testing.T) {
		// Create budget
		budget := types.TagBudget{
			Id:          1,
			Tag:         "golang",
			PoolBalance: "10000",
			Active:      true,
		}
		f.keeper.TagBudget.Set(f.ctx, 1, budget)

		resp, err := qs.TagBudgetByTag(f.ctx, &types.QueryTagBudgetByTagRequest{Tag: "golang"})
		require.NoError(t, err)
		require.Equal(t, uint64(1), resp.BudgetId)
		require.Equal(t, "10000", resp.PoolBalance)
		require.True(t, resp.Active)
	})

	t.Run("filter by specific tag", func(t *testing.T) {
		// Create multiple budgets for different tags
		budget1 := types.TagBudget{
			Id:          10,
			Tag:         "rust",
			PoolBalance: "5000",
			Active:      true,
		}
		budget2 := types.TagBudget{
			Id:          11,
			Tag:         "python",
			PoolBalance: "7500",
			Active:      false,
		}
		f.keeper.TagBudget.Set(f.ctx, 10, budget1)
		f.keeper.TagBudget.Set(f.ctx, 11, budget2)

		// Query for python tag
		resp, err := qs.TagBudgetByTag(f.ctx, &types.QueryTagBudgetByTagRequest{Tag: "python"})
		require.NoError(t, err)
		require.Equal(t, uint64(11), resp.BudgetId)
		require.Equal(t, "7500", resp.PoolBalance)
		require.False(t, resp.Active)
	})

	t.Run("inactive budget", func(t *testing.T) {
		// Create inactive budget
		budget := types.TagBudget{
			Id:          20,
			Tag:         "javascript",
			PoolBalance: "2000",
			Active:      false,
		}
		f.keeper.TagBudget.Set(f.ctx, 20, budget)

		resp, err := qs.TagBudgetByTag(f.ctx, &types.QueryTagBudgetByTagRequest{Tag: "javascript"})
		require.NoError(t, err)
		require.Equal(t, uint64(20), resp.BudgetId)
		require.False(t, resp.Active)
	})
}
