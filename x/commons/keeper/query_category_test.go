package keeper_test

import (
	"testing"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestQueryGetCategory_Nil(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	q := keeper.NewQueryServerImpl(k)

	_, err := q.GetCategory(ctx, nil)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestQueryGetCategory_NotFound(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	q := keeper.NewQueryServerImpl(k)

	_, err := q.GetCategory(ctx, &types.QueryGetCategoryRequest{CategoryId: 99})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestQueryGetCategory_Found(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	q := keeper.NewQueryServerImpl(k)

	cat := types.Category{CategoryId: 3, Title: "Tech", Description: "tech talk"}
	require.NoError(t, k.Category.Set(ctx, cat.CategoryId, cat))

	resp, err := q.GetCategory(ctx, &types.QueryGetCategoryRequest{CategoryId: 3})
	require.NoError(t, err)
	require.Equal(t, cat, resp.Category)
}

func TestQueryListCategory_Nil(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	q := keeper.NewQueryServerImpl(k)

	_, err := q.ListCategory(ctx, nil)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestQueryListCategory_Empty(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	q := keeper.NewQueryServerImpl(k)

	resp, err := q.ListCategory(ctx, &types.QueryAllCategoryRequest{})
	require.NoError(t, err)
	require.Empty(t, resp.Category)
}

func TestQueryListCategory_Multiple(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	q := keeper.NewQueryServerImpl(k)

	require.NoError(t, k.Category.Set(ctx, 1, types.Category{CategoryId: 1, Title: "A"}))
	require.NoError(t, k.Category.Set(ctx, 2, types.Category{CategoryId: 2, Title: "B"}))
	require.NoError(t, k.Category.Set(ctx, 3, types.Category{CategoryId: 3, Title: "C"}))

	resp, err := q.ListCategory(ctx, &types.QueryAllCategoryRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Category, 3)
}
