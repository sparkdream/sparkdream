package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
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

	t.Run("registered sentinel can pin", func(t *testing.T) {
		// PinReply requires a BondedRole record (ROLE_TYPE_FORUM_SENTINEL) in
		// non-DEMOTED status, matching hide/lock/move. Register the sentinel
		// via the helper so the BondedRole lookup succeeds.
		f.createTestSentinel(t, testCreator, "2000")

		rootPost := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator2, rootPost.PostId, 0)

		msg := &types.MsgPinReply{
			Creator:  testCreator,
			ThreadId: rootPost.PostId,
			ReplyId:  reply.PostId,
		}
		_, err := f.msgServer.PinReply(f.ctx, msg)
		require.NoError(t, err)
	})

	t.Run("non-sentinel cannot pin", func(t *testing.T) {
		rootPost := f.createTestPost(t, testCreator2, 0, 0)
		reply := f.createTestPost(t, testCreator, rootPost.PostId, 0)

		msg := &types.MsgPinReply{
			Creator:  testCreator2, // not registered as a sentinel
			ThreadId: rootPost.PostId,
			ReplyId:  reply.PostId,
		}
		_, err := f.msgServer.PinReply(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotSentinel)
	})

	t.Run("demoted sentinel cannot pin", func(t *testing.T) {
		// Register sentinel with DEMOTED status — bypass attempted in
		// FORUM-S2-5 must be rejected.
		if f.repKeeper.sentinels == nil {
			f.repKeeper.sentinels = make(map[string]reptypes.BondedRole)
		}
		f.repKeeper.sentinels[testSentinel] = reptypes.BondedRole{
			Address:            testSentinel,
			CurrentBond:        "2000",
			TotalCommittedBond: "0",
			BondStatus:         reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED,
		}

		rootPost := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator2, rootPost.PostId, 0)

		msg := &types.MsgPinReply{
			Creator:  testSentinel,
			ThreadId: rootPost.PostId,
			ReplyId:  reply.PostId,
		}
		_, err := f.msgServer.PinReply(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrSentinelDemoted)
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
