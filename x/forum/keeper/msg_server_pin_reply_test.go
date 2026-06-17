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
		f.createTestSentinel(t, testCreator, "2000000000")

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

	t.Run("unbonding sentinel cannot pin", func(t *testing.T) {
		// Once unbond is initiated the bond drains over a cooldown but the
		// holder must not back fresh moderation with bond already pledged to
		// leave — liability-containment side of the unbond-cooldown design.
		if f.repKeeper.sentinels == nil {
			f.repKeeper.sentinels = make(map[string]reptypes.BondedRole)
		}
		f.repKeeper.sentinels[testSentinel] = reptypes.BondedRole{
			Address:             testSentinel,
			CurrentBond:         "2000",
			TotalCommittedBond:  "0",
			PendingUnbondAmount: "2000",
			BondStatus:          reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING,
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

	// Same ephemeral block as MsgPinPost — replies that are still under TTL
	// must be promoted with MakePostPermanent before they can be featured.
	t.Run("cannot pin ephemeral reply (ErrCannotPinEphemeral)", func(t *testing.T) {
		rootPost := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator2, rootPost.PostId, 0)
		reply.ExpirationTime = 9999999999
		require.NoError(t, f.keeper.Post.Set(f.ctx, reply.PostId, reply))

		msg := &types.MsgPinReply{
			Creator:  authority,
			ThreadId: rootPost.PostId,
			ReplyId:  reply.PostId,
		}
		_, err := f.msgServer.PinReply(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrCannotPinEphemeral)
	})
}

// TestMsgServerPinReplyCounters covers the SentinelActivity pin counters that
// feed the "Total Pins" UI surface and epoch curation rewards. Regression for
// the bug where PinReply never incremented total_pins/epoch_pins, so the
// counter always read 0 regardless of how many pins a sentinel made.
func TestPinReplyBondLifecycle(t *testing.T) {
	f := initFixture(t)
	authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	f.createTestSentinel(t, testSentinel, "2000000000")
	root := f.createTestPost(t, testCreator, 0, 0)
	reply := f.createTestPost(t, testCreator2, root.PostId, 0)

	committed := func(addr string) string {
		return f.repKeeper.sentinels[addr].TotalCommittedBond
	}
	recordFor := func(threadID, replyID uint64) *types.PinnedReplyRecord {
		md, err := f.keeper.ThreadMetadata.Get(f.ctx, threadID)
		require.NoError(t, err)
		for _, r := range md.PinnedRecords {
			if r.PostId == replyID {
				return r
			}
		}
		return nil
	}

	slash := types.DefaultParams().SentinelSlashAmountOrDefault().String()

	// Sentinel pin reserves the slash amount and snapshots it on the record.
	_, err := f.msgServer.PinReply(f.ctx, &types.MsgPinReply{
		Creator: testSentinel, ThreadId: root.PostId, ReplyId: reply.PostId,
	})
	require.NoError(t, err)
	require.Equal(t, slash, committed(testSentinel), "pin must reserve the slash bond")
	require.Equal(t, slash, recordFor(root.PostId, reply.PostId).CommittedAmount)

	// Self-unpin (no dispute) releases the reservation.
	_, err = f.msgServer.UnpinReply(f.ctx, &types.MsgUnpinReply{
		Creator: testSentinel, ThreadId: root.PostId, ReplyId: reply.PostId,
	})
	require.NoError(t, err)
	require.Equal(t, "0", committed(testSentinel), "unpin must release the reserved bond")

	// Governance pin reserves nothing.
	root2 := f.createTestPost(t, testCreator, 0, 0)
	govReply := f.createTestPost(t, testCreator2, root2.PostId, 0)
	_, err = f.msgServer.PinReply(f.ctx, &types.MsgPinReply{
		Creator: authority, ThreadId: root2.PostId, ReplyId: govReply.PostId,
	})
	require.NoError(t, err)
	require.Equal(t, "", recordFor(root2.PostId, govReply.PostId).CommittedAmount,
		"gov pin must not reserve bond")
}

func TestMsgServerPinReplyCounters(t *testing.T) {
	f := initFixture(t)
	authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	f.createTestSentinel(t, testSentinel, "2000000000")
	root := f.createTestPost(t, testCreator, 0, 0)
	r1 := f.createTestPost(t, testCreator2, root.PostId, 0)
	r2 := f.createTestPost(t, testCreator2, root.PostId, 0)

	pins := func(addr string) types.SentinelActivity {
		act, err := f.keeper.SentinelActivity.Get(f.ctx, addr)
		require.NoError(t, err)
		return act
	}

	// First sentinel pin: total_pins and epoch_pins go 0 -> 1.
	_, err := f.msgServer.PinReply(f.ctx, &types.MsgPinReply{
		Creator: testSentinel, ThreadId: root.PostId, ReplyId: r1.PostId,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1), pins(testSentinel).TotalPins)
	require.Equal(t, uint64(1), pins(testSentinel).EpochPins)

	// Second pin (different reply, same sentinel): both counters advance to 2.
	_, err = f.msgServer.PinReply(f.ctx, &types.MsgPinReply{
		Creator: testSentinel, ThreadId: root.PostId, ReplyId: r2.PostId,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(2), pins(testSentinel).TotalPins)
	require.Equal(t, uint64(2), pins(testSentinel).EpochPins)

	// Unpin is not a moderation action: lifetime/epoch counters are unchanged
	// (mirrors unlock/unhide, which never decrement total_* counters).
	_, err = f.msgServer.UnpinReply(f.ctx, &types.MsgUnpinReply{
		Creator: testSentinel, ThreadId: root.PostId, ReplyId: r1.PostId,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(2), pins(testSentinel).TotalPins)
	require.Equal(t, uint64(2), pins(testSentinel).EpochPins)

	// Repinning the same reply counts again: this is the pin/unpin/repin flow
	// the bug report described, which previously still showed 0.
	_, err = f.msgServer.PinReply(f.ctx, &types.MsgPinReply{
		Creator: testSentinel, ThreadId: root.PostId, ReplyId: r1.PostId,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(3), pins(testSentinel).TotalPins)
	require.Equal(t, uint64(3), pins(testSentinel).EpochPins)

	// Governance pins are not sentinel activity: they must not create or
	// increment a SentinelActivity record for the gov authority, and must not
	// touch the sentinel's counters.
	root2 := f.createTestPost(t, testCreator, 0, 0)
	govReply := f.createTestPost(t, testCreator2, root2.PostId, 0)
	_, err = f.msgServer.PinReply(f.ctx, &types.MsgPinReply{
		Creator: authority, ThreadId: root2.PostId, ReplyId: govReply.PostId,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(3), pins(testSentinel).TotalPins)
	require.Equal(t, uint64(3), pins(testSentinel).EpochPins)

	_, govErr := f.keeper.SentinelActivity.Get(f.ctx, authority)
	require.Error(t, govErr, "gov pin must not create a SentinelActivity record")
}
