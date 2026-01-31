package keeper_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
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

	t.Run("archived thread not found", func(t *testing.T) {
		msg := &types.MsgUnarchiveThread{
			Creator: testCreator,
			RootId:  999,
		}
		_, err := f.msgServer.UnarchiveThread(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrArchivedThreadNotFound)
	})

	t.Run("unarchive cooldown", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		now := f.sdkCtx().BlockTime().Unix()

		// Create archived thread with recent archive time
		archived := types.ArchivedThread{
			RootId:         post.PostId,
			ArchivedAt:     now, // Just archived
			PostCount:      1,
			CompressedData: []byte{},
		}
		f.keeper.ArchivedThread.Set(f.ctx, post.PostId, archived)

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

		// Create compressed posts data
		posts := []types.Post{post}
		jsonData, _ := json.Marshal(posts)
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		gz.Write(jsonData)
		gz.Close()

		// Create archived thread (old enough to unarchive)
		archived := types.ArchivedThread{
			RootId:         post.PostId,
			ArchivedAt:     now - types.DefaultUnarchiveCooldown - 1, // Cooldown passed
			PostCount:      1,
			CompressedData: buf.Bytes(),
		}
		f.keeper.ArchivedThread.Set(f.ctx, post.PostId, archived)

		// Delete the original post to simulate archived state
		f.keeper.Post.Remove(f.ctx, post.PostId)

		msg := &types.MsgUnarchiveThread{
			Creator: testCreator,
			RootId:  post.PostId,
		}
		_, err := f.msgServer.UnarchiveThread(f.ctx, msg)
		require.NoError(t, err)

		// Verify post was restored
		restoredPost, err := f.keeper.Post.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.Equal(t, post.Author, restoredPost.Author)

		// Verify archived thread record was removed
		_, err = f.keeper.ArchivedThread.Get(f.ctx, post.PostId)
		require.Error(t, err)
	})
}
