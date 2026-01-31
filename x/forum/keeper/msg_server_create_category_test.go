package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestMsgServerCreateCategory(t *testing.T) {
	f := initFixture(t)

	// Get governance authority address
	authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	t.Run("invalid creator address", func(t *testing.T) {
		msg := &types.MsgCreateCategory{
			Creator:     "invalid",
			Title:       "Test Category",
			Description: "Test Description",
		}
		_, err := f.msgServer.CreateCategory(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("not governance authority", func(t *testing.T) {
		msg := &types.MsgCreateCategory{
			Creator:     testCreator,
			Title:       "Test Category",
			Description: "Test Description",
		}
		_, err := f.msgServer.CreateCategory(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "only governance authority can create categories")
	})

	t.Run("empty title", func(t *testing.T) {
		msg := &types.MsgCreateCategory{
			Creator:     authority,
			Title:       "",
			Description: "Test Description",
		}
		_, err := f.msgServer.CreateCategory(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "title cannot be empty")
	})

	t.Run("title too long", func(t *testing.T) {
		longTitle := make([]byte, 257)
		for i := range longTitle {
			longTitle[i] = 'a'
		}
		msg := &types.MsgCreateCategory{
			Creator:     authority,
			Title:       string(longTitle),
			Description: "Test Description",
		}
		_, err := f.msgServer.CreateCategory(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "title exceeds 256 characters")
	})

	t.Run("description too long", func(t *testing.T) {
		longDesc := make([]byte, 2049)
		for i := range longDesc {
			longDesc[i] = 'a'
		}
		msg := &types.MsgCreateCategory{
			Creator:     authority,
			Title:       "Test Category",
			Description: string(longDesc),
		}
		_, err := f.msgServer.CreateCategory(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "description exceeds 2048 characters")
	})

	t.Run("successful creation", func(t *testing.T) {
		msg := &types.MsgCreateCategory{
			Creator:          authority,
			Title:            "General Discussion",
			Description:      "A place for general discussion",
			MembersOnlyWrite: true,
			AdminOnlyWrite:   false,
		}
		_, err := f.msgServer.CreateCategory(f.ctx, msg)
		require.NoError(t, err)

		// Verify category was created
		var found bool
		f.keeper.Category.Walk(f.ctx, nil, func(key uint64, cat types.Category) (bool, error) {
			if cat.Title == "General Discussion" {
				found = true
				require.Equal(t, "A place for general discussion", cat.Description)
				require.True(t, cat.MembersOnlyWrite)
				require.False(t, cat.AdminOnlyWrite)
				return true, nil
			}
			return false, nil
		})
		require.True(t, found, "category should be created")
	})
}
