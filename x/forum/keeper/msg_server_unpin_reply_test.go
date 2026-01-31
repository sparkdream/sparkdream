package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestMsgServerUnpinReply(t *testing.T) {
	f := initFixture(t)
	authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	t.Run("invalid creator address", func(t *testing.T) {
		msg := &types.MsgUnpinReply{
			Creator:  "invalid",
			ThreadId: 1,
			ReplyId:  2,
		}
		_, err := f.msgServer.UnpinReply(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("thread metadata not found", func(t *testing.T) {
		msg := &types.MsgUnpinReply{
			Creator:  testCreator,
			ThreadId: 9999,
			ReplyId:  2,
		}
		_, err := f.msgServer.UnpinReply(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrPostNotFound)
	})

	t.Run("thread metadata not found with post", func(t *testing.T) {
		rootPost := f.createTestPost(t, testCreator, 0, 0)

		msg := &types.MsgUnpinReply{
			Creator:  authority,
			ThreadId: rootPost.PostId,
			ReplyId:  2,
		}
		_, err := f.msgServer.UnpinReply(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrPostNotFound)
	})

	t.Run("reply not pinned", func(t *testing.T) {
		rootPost := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator2, rootPost.PostId, 0)

		// Create thread metadata with different pinned reply
		metadata := types.ThreadMetadata{
			ThreadId:       rootPost.PostId,
			PinnedReplyIds: []uint64{999}, // Different reply pinned
			PinnedRecords:  []*types.PinnedReplyRecord{},
		}
		f.keeper.ThreadMetadata.Set(f.ctx, rootPost.PostId, metadata)

		msg := &types.MsgUnpinReply{
			Creator:  authority,
			ThreadId: rootPost.PostId,
			ReplyId:  reply.PostId,
		}
		_, err := f.msgServer.UnpinReply(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotPinned)
	})

	t.Run("governance authority unpins reply", func(t *testing.T) {
		rootPost := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator2, rootPost.PostId, 0)

		// Create thread metadata with reply pinned
		metadata := types.ThreadMetadata{
			ThreadId:       rootPost.PostId,
			PinnedReplyIds: []uint64{reply.PostId},
			PinnedRecords: []*types.PinnedReplyRecord{{
				PostId:   reply.PostId,
				PinnedBy: authority,
			}},
		}
		f.keeper.ThreadMetadata.Set(f.ctx, rootPost.PostId, metadata)

		msg := &types.MsgUnpinReply{
			Creator:  authority,
			ThreadId: rootPost.PostId,
			ReplyId:  reply.PostId,
		}
		_, err := f.msgServer.UnpinReply(f.ctx, msg)
		require.NoError(t, err)

		// Verify reply was unpinned
		updatedMetadata, err := f.keeper.ThreadMetadata.Get(f.ctx, rootPost.PostId)
		require.NoError(t, err)
		require.NotContains(t, updatedMetadata.PinnedReplyIds, reply.PostId)
	})
}
