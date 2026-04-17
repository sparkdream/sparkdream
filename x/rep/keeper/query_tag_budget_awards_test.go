package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
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
		award := types.TagBudgetAward{
			Id: 1, BudgetId: 5, PostId: 100, Recipient: "alice", Amount: "500",
		}
		require.NoError(t, f.keeper.TagBudgetAward.Set(f.ctx, 1, award))

		resp, err := qs.TagBudgetAwards(f.ctx, &types.QueryTagBudgetAwardsRequest{BudgetId: 5})
		require.NoError(t, err)
		require.Equal(t, uint64(100), resp.PostId)
		require.Equal(t, "alice", resp.Recipient)
		require.Equal(t, "500", resp.Amount)
	})
}
