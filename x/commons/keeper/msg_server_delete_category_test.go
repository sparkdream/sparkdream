package keeper_test

import (
	"context"
	"testing"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// mockForumKeeper satisfies types.ForumKeeper for the DeleteCategory tests.
// `posts` keyed by category_id returns whether at least one post exists.
type mockForumKeeper struct {
	posts map[uint64]bool
}

func (m *mockForumKeeper) HasPostInCategory(_ context.Context, categoryID uint64) (bool, error) {
	if m == nil {
		return false, nil
	}
	return m.posts[categoryID], nil
}

// setupDeleteCategory creates a fresh commons keeper, wires a mock ForumKeeper,
// and pre-creates a category owned by the gov authority for the test to act on.
func setupDeleteCategory(t *testing.T) (keeper.Keeper, sdk.Context, *mockForumKeeper, uint64) {
	t.Helper()
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	mockForum := &mockForumKeeper{posts: map[uint64]bool{}}
	k.SetForumKeeper(mockForum)

	resp, err := msgServer.CreateCategory(ctx, &types.MsgCreateCategory{
		Creator: k.GetAuthorityString(),
		Title:   "Doomed",
	})
	require.NoError(t, err)

	return k, ctx, mockForum, resp.CategoryId
}

func TestDeleteCategory_Success_AsAuthority(t *testing.T) {
	k, ctx, _, categoryID := setupDeleteCategory(t)
	msgServer := keeper.NewMsgServerImpl(k)

	_, err := msgServer.DeleteCategory(ctx, &types.MsgDeleteCategory{
		Creator:    k.GetAuthorityString(),
		CategoryId: categoryID,
	})
	require.NoError(t, err)

	_, ok := k.GetCategory(ctx, categoryID)
	require.False(t, ok, "category should be removed from store")
}

func TestDeleteCategory_Success_AsOperationsCommittee(t *testing.T) {
	k, ctx, _, categoryID := setupDeleteCategory(t)
	msgServer := keeper.NewMsgServerImpl(k)

	member := sdk.AccAddress([]byte("ops_committee_mbr___")).String()
	require.NoError(t, k.AddMember(ctx, "Commons Operations Committee", types.Member{
		Address: member, Weight: "1",
	}))

	_, err := msgServer.DeleteCategory(ctx, &types.MsgDeleteCategory{
		Creator:    member,
		CategoryId: categoryID,
	})
	require.NoError(t, err)
}

func TestDeleteCategory_InvalidCreator(t *testing.T) {
	k, ctx, _, categoryID := setupDeleteCategory(t)
	msgServer := keeper.NewMsgServerImpl(k)

	_, err := msgServer.DeleteCategory(ctx, &types.MsgDeleteCategory{
		Creator:    "not-a-bech32-address",
		CategoryId: categoryID,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid creator address")
}

func TestDeleteCategory_Unauthorized(t *testing.T) {
	k, ctx, _, categoryID := setupDeleteCategory(t)
	msgServer := keeper.NewMsgServerImpl(k)

	random := sdk.AccAddress([]byte("random_creator______")).String()

	_, err := msgServer.DeleteCategory(ctx, &types.MsgDeleteCategory{
		Creator:    random,
		CategoryId: categoryID,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "only governance")
}

func TestDeleteCategory_NotFound(t *testing.T) {
	k, ctx, _, _ := setupDeleteCategory(t)
	msgServer := keeper.NewMsgServerImpl(k)

	_, err := msgServer.DeleteCategory(ctx, &types.MsgDeleteCategory{
		Creator:    k.GetAuthorityString(),
		CategoryId: 9_999_999,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestDeleteCategory_RefusesWhenPostsExist(t *testing.T) {
	k, ctx, mockForum, categoryID := setupDeleteCategory(t)
	msgServer := keeper.NewMsgServerImpl(k)

	mockForum.posts[categoryID] = true

	_, err := msgServer.DeleteCategory(ctx, &types.MsgDeleteCategory{
		Creator:    k.GetAuthorityString(),
		CategoryId: categoryID,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "still has forum posts")

	// Category must remain in store.
	_, ok := k.GetCategory(ctx, categoryID)
	require.True(t, ok, "category should NOT be removed when posts exist")
}
