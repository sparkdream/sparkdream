package keeper_test

import (
	"testing"

	"sparkdream/x/commons/types"

	"github.com/stretchr/testify/require"
)

func TestGetCategory_NotFound(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	cat, ok := k.GetCategory(ctx, 42)
	require.False(t, ok)
	require.Equal(t, types.Category{}, cat)
}

func TestGetCategory_Found(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	stored := types.Category{
		CategoryId:       7,
		Title:            "General",
		Description:      "general discussion",
		MembersOnlyWrite: true,
	}
	require.NoError(t, k.Category.Set(ctx, stored.CategoryId, stored))

	got, ok := k.GetCategory(ctx, stored.CategoryId)
	require.True(t, ok)
	require.Equal(t, stored, got)
}

func TestHasCategory(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	require.False(t, k.HasCategory(ctx, 1))

	require.NoError(t, k.Category.Set(ctx, 1, types.Category{CategoryId: 1, Title: "T"}))
	require.True(t, k.HasCategory(ctx, 1))
}
