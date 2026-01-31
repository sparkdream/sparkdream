package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestMsgServerPinReply(t *testing.T) {
	f := initFixture(t)
	authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	t.Run("invalid creator address", func(t *testing.T) {
		msg := &types.MsgPinReply{
			Creator:  "invalid",
			ThreadId: 1,
			ReplyId:  2,
		}
		_, err := f.msgServer.PinReply(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("any user with sentinel status can pin", func(t *testing.T) {
		// Note: With repKeeper=nil stubs, GetRepTier returns 5 and GetSentinelBond returns 2000
		// So all users are effectively sentinels in tests
		rootPost := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator2, rootPost.PostId, 0)

		msg := &types.MsgPinReply{
			Creator:  testCreator,
			ThreadId: rootPost.PostId,
			ReplyId:  reply.PostId,
		}
		_, err := f.msgServer.PinReply(f.ctx, msg)
		require.NoError(t, err) // Passes due to sentinel stub returning high values
	})

	t.Run("thread not found", func(t *testing.T) {
		msg := &types.MsgPinReply{
			Creator:  authority,
			ThreadId: 999,
			ReplyId:  2,
		}
		_, err := f.msgServer.PinReply(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrPostNotFound)
	})

	t.Run("not a root post", func(t *testing.T) {
		rootPost := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator, rootPost.PostId, 0)

		msg := &types.MsgPinReply{
			Creator:  authority,
			ThreadId: reply.PostId, // reply is not a root
			ReplyId:  rootPost.PostId,
		}
		_, err := f.msgServer.PinReply(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotRootPost)
	})

	t.Run("reply not found", func(t *testing.T) {
		rootPost := f.createTestPost(t, testCreator, 0, 0)

		msg := &types.MsgPinReply{
			Creator:  authority,
			ThreadId: rootPost.PostId,
			ReplyId:  999,
		}
		_, err := f.msgServer.PinReply(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrPostNotFound)
	})

	t.Run("cannot pin root post as reply", func(t *testing.T) {
		rootPost := f.createTestPost(t, testCreator, 0, 0)

		msg := &types.MsgPinReply{
			Creator:  authority,
			ThreadId: rootPost.PostId,
			ReplyId:  rootPost.PostId,
		}
		_, err := f.msgServer.PinReply(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotReplyInThread)
	})

	t.Run("governance authority pins reply", func(t *testing.T) {
		rootPost := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator2, rootPost.PostId, 0)

		msg := &types.MsgPinReply{
			Creator:  authority,
			ThreadId: rootPost.PostId,
			ReplyId:  reply.PostId,
		}
		_, err := f.msgServer.PinReply(f.ctx, msg)
		require.NoError(t, err)

		// Verify thread metadata updated
		metadata, err := f.keeper.ThreadMetadata.Get(f.ctx, rootPost.PostId)
		require.NoError(t, err)
		require.Contains(t, metadata.PinnedReplyIds, reply.PostId)
	})

	t.Run("cannot pin already pinned reply", func(t *testing.T) {
		rootPost := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator2, rootPost.PostId, 0)

		// Create thread metadata with reply already pinned
		metadata := types.ThreadMetadata{
			ThreadId:       rootPost.PostId,
			PinnedReplyIds: []uint64{reply.PostId},
			PinnedRecords: []*types.PinnedReplyRecord{{
				PostId:   reply.PostId,
				PinnedBy: authority,
			}},
		}
		f.keeper.ThreadMetadata.Set(f.ctx, rootPost.PostId, metadata)

		msg := &types.MsgPinReply{
			Creator:  authority,
			ThreadId: rootPost.PostId,
			ReplyId:  reply.PostId,
		}
		_, err := f.msgServer.PinReply(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrAlreadyPinned)
	})

	t.Run("cannot pin deleted reply", func(t *testing.T) {
		rootPost := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator2, rootPost.PostId, 0)
		reply.Status = types.PostStatus_POST_STATUS_DELETED
		f.keeper.Post.Set(f.ctx, reply.PostId, reply)

		msg := &types.MsgPinReply{
			Creator:  authority,
			ThreadId: rootPost.PostId,
			ReplyId:  reply.PostId,
		}
		_, err := f.msgServer.PinReply(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrPostStatus)
	})
}
