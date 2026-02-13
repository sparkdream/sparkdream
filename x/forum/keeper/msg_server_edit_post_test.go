package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestMsgServerEditPost(t *testing.T) {
	f := initFixture(t)

	t.Run("invalid creator address", func(t *testing.T) {
		msg := &types.MsgEditPost{
			Creator:    "invalid",
			PostId:     1,
			NewContent: "Updated content",
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("post not found", func(t *testing.T) {
		msg := &types.MsgEditPost{
			Creator:    testCreator,
			PostId:     999,
			NewContent: "Updated content",
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrPostNotFound)
	})

	t.Run("not post author", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		msg := &types.MsgEditPost{
			Creator:    testCreator2,
			PostId:     post.PostId,
			NewContent: "Updated content",
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotPostAuthor)
	})

	t.Run("cannot edit hidden post", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		post.Status = types.PostStatus_POST_STATUS_HIDDEN
		f.keeper.Post.Set(f.ctx, post.PostId, post)

		msg := &types.MsgEditPost{
			Creator:    testCreator,
			PostId:     post.PostId,
			NewContent: "Updated content",
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrCannotEditHiddenPost)
	})

	t.Run("cannot edit deleted post", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		post.Status = types.PostStatus_POST_STATUS_DELETED
		f.keeper.Post.Set(f.ctx, post.PostId, post)

		msg := &types.MsgEditPost{
			Creator:    testCreator,
			PostId:     post.PostId,
			NewContent: "Updated content",
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrCannotEditDeletedPost)
	})

	t.Run("cannot edit archived post", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		post.Status = types.PostStatus_POST_STATUS_ARCHIVED
		f.keeper.Post.Set(f.ctx, post.PostId, post)

		msg := &types.MsgEditPost{
			Creator:    testCreator,
			PostId:     post.PostId,
			NewContent: "Updated content",
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrPostArchived)
	})

	t.Run("empty new content", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		msg := &types.MsgEditPost{
			Creator:    testCreator,
			PostId:     post.PostId,
			NewContent: "",
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrEmptyContent)
	})

	t.Run("successful edit", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		msg := &types.MsgEditPost{
			Creator:    testCreator,
			PostId:     post.PostId,
			NewContent: "Updated content here",
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.NoError(t, err)

		// Verify post was updated
		updatedPost, err := f.keeper.Post.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.Equal(t, "Updated content here", updatedPost.Content)
		require.True(t, updatedPost.Edited)
	})

	t.Run("editing disabled", func(t *testing.T) {
		params := types.DefaultParams()
		params.EditingEnabled = false
		f.keeper.Params.Set(f.ctx, params)

		post := f.createTestPost(t, testCreator, 0, 0)

		msg := &types.MsgEditPost{
			Creator:    testCreator,
			PostId:     post.PostId,
			NewContent: "Updated content",
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrEditingDisabled)

		// Reset params
		f.keeper.Params.Set(f.ctx, types.DefaultParams())
	})
}

func TestMsgServerEditPostTags(t *testing.T) {
	f := initFixture(t)

	// Create tags in the store
	f.createTestTag(t, "golang")
	f.createTestTag(t, "cosmos-sdk")
	f.createTestTag(t, "testing")

	t.Run("successful edit with tags", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		msg := &types.MsgEditPost{
			Creator:    testCreator,
			PostId:     post.PostId,
			NewContent: "Updated content with tags",
			Tags:       []string{"golang", "cosmos-sdk"},
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.NoError(t, err)

		// Verify tags are stored
		updatedPost, err := f.keeper.Post.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.Equal(t, []string{"golang", "cosmos-sdk"}, updatedPost.Tags)
		require.True(t, updatedPost.Edited)
	})

	t.Run("edit replaces tags", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		// First edit: set tags
		msg := &types.MsgEditPost{
			Creator:    testCreator,
			PostId:     post.PostId,
			NewContent: "Content v1",
			Tags:       []string{"golang"},
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.NoError(t, err)

		// Second edit: replace tags
		msg2 := &types.MsgEditPost{
			Creator:    testCreator,
			PostId:     post.PostId,
			NewContent: "Content v2",
			Tags:       []string{"cosmos-sdk", "testing"},
		}
		_, err = f.msgServer.EditPost(f.ctx, msg2)
		require.NoError(t, err)

		updatedPost, err := f.keeper.Post.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.Equal(t, []string{"cosmos-sdk", "testing"}, updatedPost.Tags)
	})

	t.Run("edit clears tags with empty list", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		// Set tags first
		msg := &types.MsgEditPost{
			Creator:    testCreator,
			PostId:     post.PostId,
			NewContent: "Content with tags",
			Tags:       []string{"golang"},
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.NoError(t, err)

		// Clear tags by sending empty list
		msg2 := &types.MsgEditPost{
			Creator:    testCreator,
			PostId:     post.PostId,
			NewContent: "Content without tags",
		}
		_, err = f.msgServer.EditPost(f.ctx, msg2)
		require.NoError(t, err)

		updatedPost, err := f.keeper.Post.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.Empty(t, updatedPost.Tags)
	})

	t.Run("edit with nonexistent tag fails", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		msg := &types.MsgEditPost{
			Creator:    testCreator,
			PostId:     post.PostId,
			NewContent: "Updated content",
			Tags:       []string{"nonexistent-tag"},
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "tag not found")
	})

	t.Run("edit with too many tags fails", func(t *testing.T) {
		f.createTestTag(t, "alpha")
		f.createTestTag(t, "beta")
		f.createTestTag(t, "gamma")

		post := f.createTestPost(t, testCreator, 0, 0)

		msg := &types.MsgEditPost{
			Creator:    testCreator,
			PostId:     post.PostId,
			NewContent: "Updated content",
			Tags:       []string{"golang", "cosmos-sdk", "testing", "alpha", "beta", "gamma"},
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "tag limit exceeded")
	})
}

func TestEditPostStorageDeltaFee(t *testing.T) {
	t.Run("edit size increase charges delta", func(t *testing.T) {
		f := initFixture(t)

		// Create a post with known content
		post := f.createTestPost(t, testCreator, 0, 0)
		oldContent := post.Content // "Test content" = 12 bytes

		// Reset bank keeper tracking
		f.bankKeeper.SendCoinsFromAccountToModuleCalls = nil
		f.bankKeeper.BurnCoinsCalls = nil

		newContent := "This is much longer content now!" // 32 bytes, delta = 32 - 12 = 20
		msg := &types.MsgEditPost{
			Creator:    testCreator,
			PostId:     post.PostId,
			NewContent: newContent,
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.NoError(t, err)

		// Delta = (32 - 12) * 100 = 2000 uspark
		delta := int64(len(newContent)) - int64(len(oldContent))
		expectedFee := sdk.NewCoin("uspark", math.NewInt(delta*100))
		require.GreaterOrEqual(t, len(f.bankKeeper.SendCoinsFromAccountToModuleCalls), 1)
		require.Equal(t, sdk.NewCoins(expectedFee), f.bankKeeper.SendCoinsFromAccountToModuleCalls[0].Amt)
		require.GreaterOrEqual(t, len(f.bankKeeper.BurnCoinsCalls), 1)
		require.Equal(t, sdk.NewCoins(expectedFee), f.bankKeeper.BurnCoinsCalls[0].Amt)
	})

	t.Run("edit size decrease charges nothing", func(t *testing.T) {
		f := initFixture(t)

		// Create a post with known content
		post := f.createTestPost(t, testCreator, 0, 0)
		// Default "Test content" = 12 bytes

		// Reset bank keeper tracking
		f.bankKeeper.SendCoinsFromAccountToModuleCalls = nil
		f.bankKeeper.BurnCoinsCalls = nil

		msg := &types.MsgEditPost{
			Creator:    testCreator,
			PostId:     post.PostId,
			NewContent: "Short", // 5 bytes < 12 bytes
		}
		_, err := f.msgServer.EditPost(f.ctx, msg)
		require.NoError(t, err)

		// No delta fee should be charged (size decreased)
		require.Len(t, f.bankKeeper.SendCoinsFromAccountToModuleCalls, 0)
		require.Len(t, f.bankKeeper.BurnCoinsCalls, 0)
	})
}
