package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

// TestHasPostInCategory pins the semantics that x/commons' MsgDeleteCategory
// depends on: a category is considered "empty enough to delete" once every
// post that references it is in a terminal status (DELETED, HIDDEN,
// ARCHIVED, or UNSPECIFIED). Only POST_STATUS_ACTIVE posts count as live
// references. Without this filter, an admin could never delete a category
// that ever held a post — soft-deleted tombstones would block forever.
func TestHasPostInCategory(t *testing.T) {
	const (
		emptyCat   = uint64(100)
		liveCat    = uint64(200)
		mixedCat   = uint64(300)
		otherCat   = uint64(400)
		archiveCat = uint64(500)
	)

	t.Run("empty category", func(t *testing.T) {
		f := initFixture(t)
		has, err := f.keeper.HasPostInCategory(f.ctx, emptyCat)
		require.NoError(t, err)
		require.False(t, has, "category with no posts should report empty")
	})

	t.Run("active post blocks", func(t *testing.T) {
		f := initFixture(t)
		f.createTestPost(t, testCreator, 0, liveCat) // ACTIVE by default

		has, err := f.keeper.HasPostInCategory(f.ctx, liveCat)
		require.NoError(t, err)
		require.True(t, has, "ACTIVE post must keep the category alive")
	})

	t.Run("only-tombstoned posts do not block", func(t *testing.T) {
		f := initFixture(t)

		// Seed one post per terminal status. None should count as "live".
		for _, status := range []types.PostStatus{
			types.PostStatus_POST_STATUS_DELETED,
			types.PostStatus_POST_STATUS_HIDDEN,
			types.PostStatus_POST_STATUS_ARCHIVED,
		} {
			post := f.createTestPost(t, testCreator, 0, mixedCat)
			post.Status = status
			require.NoError(t, f.keeper.Post.Set(f.ctx, post.PostId, post))
		}

		has, err := f.keeper.HasPostInCategory(f.ctx, mixedCat)
		require.NoError(t, err)
		require.False(t, has, "tombstoned posts (DELETED/HIDDEN/ARCHIVED) must not block category deletion")
	})

	t.Run("one active among tombstones still blocks", func(t *testing.T) {
		f := initFixture(t)

		// Two tombstones …
		dead := f.createTestPost(t, testCreator, 0, mixedCat)
		dead.Status = types.PostStatus_POST_STATUS_DELETED
		require.NoError(t, f.keeper.Post.Set(f.ctx, dead.PostId, dead))

		hidden := f.createTestPost(t, testCreator, 0, mixedCat)
		hidden.Status = types.PostStatus_POST_STATUS_HIDDEN
		require.NoError(t, f.keeper.Post.Set(f.ctx, hidden.PostId, hidden))

		// … plus one live post in the same category.
		f.createTestPost(t, testCreator, 0, mixedCat)

		has, err := f.keeper.HasPostInCategory(f.ctx, mixedCat)
		require.NoError(t, err)
		require.True(t, has, "a single ACTIVE post must still block, even when tombstones exist alongside")
	})

	t.Run("posts in other categories do not leak across", func(t *testing.T) {
		f := initFixture(t)

		// Active post in otherCat must not affect liveCat / mixedCat queries.
		f.createTestPost(t, testCreator, 0, otherCat)

		hasOther, err := f.keeper.HasPostInCategory(f.ctx, otherCat)
		require.NoError(t, err)
		require.True(t, hasOther, "otherCat has its own active post")

		hasLive, err := f.keeper.HasPostInCategory(f.ctx, liveCat)
		require.NoError(t, err)
		require.False(t, hasLive, "liveCat should be unaffected by otherCat's content")
	})

	t.Run("archived-only category reports empty", func(t *testing.T) {
		f := initFixture(t)
		post := f.createTestPost(t, testCreator, 0, archiveCat)
		post.Status = types.PostStatus_POST_STATUS_ARCHIVED
		require.NoError(t, f.keeper.Post.Set(f.ctx, post.PostId, post))

		has, err := f.keeper.HasPostInCategory(f.ctx, archiveCat)
		require.NoError(t, err)
		require.False(t, has, "ARCHIVED-only category must report empty for deletion purposes")
	})
}
