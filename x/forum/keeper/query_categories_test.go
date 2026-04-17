package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryCategories(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.Categories(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty list", func(t *testing.T) {
		resp, err := qs.Categories(f.ctx, &types.QueryCategoriesRequest{})
		require.NoError(t, err)
		require.Empty(t, resp.Categories)
	})

	t.Run("list with categories", func(t *testing.T) {
		cat := f.createTestCategory(t, "Test Category")

		resp, err := qs.Categories(f.ctx, &types.QueryCategoriesRequest{})
		require.NoError(t, err)
		require.NotEmpty(t, resp.Categories)
		found := false
		for _, c := range resp.Categories {
			if c.CategoryId == cat.CategoryId {
				found = true
				require.Equal(t, "Test Category", c.Title)
			}
		}
		require.True(t, found, "created category not returned")
	})
}
