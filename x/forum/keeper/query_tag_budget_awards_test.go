package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryTagBudgetAwards(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.TagBudgetAwards(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("zero budget_id", func(t *testing.T) {
		_, err := qs.TagBudgetAwards(f.ctx, &types.QueryTagBudgetAwardsRequest{BudgetId: 0})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no awards", func(t *testing.T) {
		resp, err := qs.TagBudgetAwards(f.ctx, &types.QueryTagBudgetAwardsRequest{BudgetId: 1})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.PostId)
		require.Empty(t, resp.Recipient)
	})

	t.Run("has awards", func(t *testing.T) {
		// Create award
		award := types.TagBudgetAward{
			Id:        1,
			BudgetId:  5,
			PostId:    100,
			Recipient: testCreator,
			Amount:    "500",
		}
		f.keeper.TagBudgetAward.Set(f.ctx, 1, award)

		resp, err := qs.TagBudgetAwards(f.ctx, &types.QueryTagBudgetAwardsRequest{BudgetId: 5})
		require.NoError(t, err)
		require.Equal(t, uint64(100), resp.PostId)
		require.Equal(t, testCreator, resp.Recipient)
		require.Equal(t, "500", resp.Amount)
	})

	t.Run("filter by budget_id", func(t *testing.T) {
		// Create awards for different budgets
		award1 := types.TagBudgetAward{
			Id:        10,
			BudgetId:  10,
			PostId:    200,
			Recipient: testCreator,
			Amount:    "100",
		}
		award2 := types.TagBudgetAward{
			Id:        11,
			BudgetId:  20,
			PostId:    300,
			Recipient: testCreator2,
			Amount:    "200",
		}
		f.keeper.TagBudgetAward.Set(f.ctx, 10, award1)
		f.keeper.TagBudgetAward.Set(f.ctx, 11, award2)

		// Query for budget 20
		resp, err := qs.TagBudgetAwards(f.ctx, &types.QueryTagBudgetAwardsRequest{BudgetId: 20})
		require.NoError(t, err)
		require.Equal(t, uint64(300), resp.PostId)
		require.Equal(t, testCreator2, resp.Recipient)
	})
}
