package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestMsgServerUnarchiveThread(t *testing.T) {
	f := initFixture(t)

	t.Run("invalid creator address", func(t *testing.T) {
		msg := &types.MsgUnarchiveThread{
			Creator: "invalid",
			RootId:  1,
		}
		_, err := f.msgServer.UnarchiveThread(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("forum paused", func(t *testing.T) {
		params := types.DefaultParams()
		params.ForumPaused = true
		f.keeper.Params.Set(f.ctx, params)

		msg := &types.MsgUnarchiveThread{
			Creator: testCreator,
			RootId:  1,
		}
		_, err := f.msgServer.UnarchiveThread(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrForumPaused)

		// Reset params
		f.keeper.Params.Set(f.ctx, types.DefaultParams())
	})

	t.Run("thread not found", func(t *testing.T) {
		msg := &types.MsgUnarchiveThread{
			Creator: testCreator,
			RootId:  999,
		}
		_, err := f.msgServer.UnarchiveThread(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("thread not archived", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		msg := &types.MsgUnarchiveThread{
			Creator: testCreator,
			RootId:  post.PostId,
		}
		_, err := f.msgServer.UnarchiveThread(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrArchivedThreadNotFound)
	})

	t.Run("unarchive cooldown", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		now := f.sdkCtx().BlockTime().Unix()

		// Set post status to ARCHIVED
		p, _ := f.keeper.Post.Get(f.ctx, post.PostId)
		p.Status = types.PostStatus_POST_STATUS_ARCHIVED
		f.keeper.Post.Set(f.ctx, post.PostId, p)

		// Create archive metadata with recent archive time
		meta := types.ArchiveMetadata{
			RootId:         post.PostId,
			ArchiveCount:   1,
			LastArchivedAt: now, // Just archived
		}
		f.keeper.ArchiveMetadata.Set(f.ctx, post.PostId, meta)

		msg := &types.MsgUnarchiveThread{
			Creator: testCreator,
			RootId:  post.PostId,
		}
		_, err := f.msgServer.UnarchiveThread(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrUnarchiveCooldown)
	})

	t.Run("successful unarchive", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		now := f.sdkCtx().BlockTime().Unix()

		// Set post status to ARCHIVED
		p, _ := f.keeper.Post.Get(f.ctx, post.PostId)
		p.Status = types.PostStatus_POST_STATUS_ARCHIVED
		f.keeper.Post.Set(f.ctx, post.PostId, p)

		// Create archive metadata (old enough to unarchive)
		meta := types.ArchiveMetadata{
			RootId:         post.PostId,
			ArchiveCount:   1,
			LastArchivedAt: now - types.DefaultUnarchiveCooldown - 1,
		}
		f.keeper.ArchiveMetadata.Set(f.ctx, post.PostId, meta)

		msg := &types.MsgUnarchiveThread{
			Creator: testCreator,
			RootId:  post.PostId,
		}
		_, err := f.msgServer.UnarchiveThread(f.ctx, msg)
		require.NoError(t, err)

		// Verify post status was restored
		restoredPost, err := f.keeper.Post.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.Equal(t, types.PostStatus_POST_STATUS_ACTIVE, restoredPost.Status)
	})

	t.Run("successful unarchive with replies", func(t *testing.T) {
		cat := f.createTestCategory(t, "UnarchiveReplies")
		root := f.createTestPost(t, testCreator, 0, cat.CategoryId)
		reply1 := f.createTestPost(t, testCreator2, root.PostId, cat.CategoryId)
		reply2 := f.createTestPost(t, testSentinel, root.PostId, cat.CategoryId)
		now := f.sdkCtx().BlockTime().Unix()

		// Set all posts to ARCHIVED status
		for _, id := range []uint64{root.PostId, reply1.PostId, reply2.PostId} {
			p, _ := f.keeper.Post.Get(f.ctx, id)
			p.Status = types.PostStatus_POST_STATUS_ARCHIVED
			f.keeper.Post.Set(f.ctx, id, p)
		}

		// Create archive metadata (old enough)
		meta := types.ArchiveMetadata{
			RootId:         root.PostId,
			ArchiveCount:   1,
			LastArchivedAt: now - types.DefaultUnarchiveCooldown - 1,
		}
		f.keeper.ArchiveMetadata.Set(f.ctx, root.PostId, meta)

		msg := &types.MsgUnarchiveThread{
			Creator: testCreator,
			RootId:  root.PostId,
		}
		_, err := f.msgServer.UnarchiveThread(f.ctx, msg)
		require.NoError(t, err)

		// Verify all posts restored to ACTIVE
		for _, id := range []uint64{root.PostId, reply1.PostId, reply2.PostId} {
			p, err := f.keeper.Post.Get(f.ctx, id)
			require.NoError(t, err)
			require.Equal(t, types.PostStatus_POST_STATUS_ACTIVE, p.Status)
		}
	})
}
