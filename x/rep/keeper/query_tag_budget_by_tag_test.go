package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
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
	})

	t.Run("no budget for tag", func(t *testing.T) {
		resp, err := qs.TagBudgetByTag(f.ctx, &types.QueryTagBudgetByTagRequest{Tag: "missing"})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.BudgetId)
		require.Empty(t, resp.PoolBalance)
	})

	t.Run("has budget", func(t *testing.T) {
		id, _ := f.keeper.TagBudgetSeq.Next(f.ctx)
		require.NoError(t, f.keeper.TagBudget.Set(f.ctx, id, types.TagBudget{
			Id: id, Tag: "golang", PoolBalance: "10000", Active: true,
		}))

		resp, err := qs.TagBudgetByTag(f.ctx, &types.QueryTagBudgetByTagRequest{Tag: "golang"})
		require.NoError(t, err)
		require.Equal(t, id, resp.BudgetId)
		require.Equal(t, "10000", resp.PoolBalance)
		require.True(t, resp.Active)
	})
}
