package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// Phase A: an author directly accepting a reply must supersede a pending sentinel
// proposal, and a later auto-confirm pass must not overwrite the author's choice
// or mint a reward.
func TestAuthorAcceptSupersedesPendingProposal(t *testing.T) {
	f := initFixture(t)
	now := int64(1_000_000)
	f.ctx = f.sdkCtx().WithBlockTime(time.Unix(now, 0))
	f.createTestSentinel(t, testSentinel, "1000000000")
	thread := f.createTestPost(t, testCreator, 0, 0)
	replyA := f.createTestPost(t, testCreator2, thread.PostId, thread.PostId)
	replyB := f.createTestPost(t, testCreator2, thread.PostId, thread.PostId)

	md := proposeReply(t, f, testSentinel, thread.PostId, replyA.PostId)
	require.Equal(t, replyA.PostId, md.ProposedReplyId)

	// Author directly accepts a DIFFERENT reply.
	ms := keeper.NewMsgServerImpl(f.keeper)
	_, err := ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{
		Creator:  testCreator,
		ThreadId: thread.PostId,
		ReplyId:  replyB.PostId,
	})
	require.NoError(t, err)

	updated, err := f.keeper.ThreadMetadata.Get(f.ctx, thread.PostId)
	require.NoError(t, err)
	require.Equal(t, replyB.PostId, updated.AcceptedReplyId)
	require.Equal(t, uint64(0), updated.ProposedReplyId, "pending proposal superseded")
	require.Equal(t, int64(0), updated.ProposalFireAt)

	has, err := f.keeper.ProposalAutoConfirmQueue.Has(f.ctx, collections.Join(md.ProposalFireAt, thread.PostId))
	require.NoError(t, err)
	require.False(t, has, "queue entry drained")

	// Auto-confirm pass past the original fire_at must not overwrite or reward.
	f.ctx = f.sdkCtx().WithBlockTime(time.Unix(md.ProposalFireAt+1, 0))
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	final, err := f.keeper.ThreadMetadata.Get(f.ctx, thread.PostId)
	require.NoError(t, err)
	require.Equal(t, replyB.PostId, final.AcceptedReplyId, "author choice preserved")
	require.Empty(t, f.repKeeper.mintDreamCalls, "no curation reward")
}

// Phase A: the EndBlocker guard drops a dangling pending proposal (without
// confirming or rewarding) when an accepted reply already exists.
func TestAutoConfirmGuardSkipsWhenAlreadyAccepted(t *testing.T) {
	f := initFixture(t)
	now := int64(1_000_000)
	f.ctx = f.sdkCtx().WithBlockTime(time.Unix(now, 0))
	f.createTestSentinel(t, testSentinel, "1000000000")
	thread := f.createTestPost(t, testCreator, 0, 0)
	reply := f.createTestPost(t, testCreator2, thread.PostId, thread.PostId)

	fireAt := now + 100
	md := types.ThreadMetadata{
		ThreadId:        thread.PostId,
		AcceptedReplyId: reply.PostId,
		AcceptedBy:      testCreator,
		AcceptedAt:      now,
		ProposedReplyId: reply.PostId,
		ProposedBy:      testSentinel,
		ProposedAt:      now,
		ProposalFireAt:  fireAt,
	}
	require.NoError(t, f.keeper.ThreadMetadata.Set(f.ctx, thread.PostId, md))
	require.NoError(t, f.keeper.ProposalAutoConfirmQueue.Set(f.ctx, collections.Join(fireAt, thread.PostId)))

	f.ctx = f.sdkCtx().WithBlockTime(time.Unix(fireAt+1, 0))
	require.NoError(t, f.keeper.EndBlocker(f.ctx))

	updated, err := f.keeper.ThreadMetadata.Get(f.ctx, thread.PostId)
	require.NoError(t, err)
	require.Equal(t, reply.PostId, updated.AcceptedReplyId, "acceptance untouched")
	require.Equal(t, uint64(0), updated.ProposedReplyId, "dangling proposal cleared")
	require.Empty(t, f.repKeeper.mintDreamCalls, "no reward")
	has, err := f.keeper.ProposalAutoConfirmQueue.Has(f.ctx, collections.Join(fireAt, thread.PostId))
	require.NoError(t, err)
	require.False(t, has)
}

// Phase B: the author can clear an accepted reply (reply_id == 0), and clearing
// when nothing is accepted errors.
func TestAuthorClearAcceptedReply(t *testing.T) {
	f := initFixture(t)
	f.ctx = f.sdkCtx().WithBlockTime(time.Unix(1_000_000, 0))
	thread := f.createTestPost(t, testCreator, 0, 0)
	reply := f.createTestPost(t, testCreator2, thread.PostId, thread.PostId)
	ms := keeper.NewMsgServerImpl(f.keeper)

	_, err := ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{Creator: testCreator, ThreadId: thread.PostId, ReplyId: reply.PostId})
	require.NoError(t, err)

	_, err = ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{Creator: testCreator, ThreadId: thread.PostId, ReplyId: 0})
	require.NoError(t, err)
	updated, err := f.keeper.ThreadMetadata.Get(f.ctx, thread.PostId)
	require.NoError(t, err)
	require.Zero(t, updated.AcceptedReplyId)

	_, err = ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{Creator: testCreator, ThreadId: thread.PostId, ReplyId: 0})
	require.ErrorIs(t, err, types.ErrNoAcceptedReply)
}

// Phase C1: a confirm records an upheld accuracy tick; rejections record
// overturned ticks and, on a streak, demote the sentinel — the same machinery
// used for overturned moderation actions.
func TestCurationAccuracyAndDemotion(t *testing.T) {
	f := initFixture(t)
	f.ctx = f.sdkCtx().WithBlockTime(time.Unix(1_000_000, 0))
	f.createTestSentinel(t, testSentinel, "1000000000")
	ms := keeper.NewMsgServerImpl(f.keeper)

	// One confirm -> one upheld tick.
	thread0 := f.createTestPost(t, testCreator, 0, 0)
	reply0 := f.createTestPost(t, testCreator2, thread0.PostId, thread0.PostId)
	proposeReply(t, f, testSentinel, thread0.PostId, reply0.PostId)
	_, err := ms.ConfirmProposedReply(f.ctx, &types.MsgConfirmProposedReply{Creator: testCreator, ThreadId: thread0.PostId})
	require.NoError(t, err)

	// Streaks + verdict tallies live on rep's shared RoleActivity record now
	// (the mock rep keeper keeps a functional copy).
	ra := f.repKeeper.roleActivities[testSentinel]
	require.Equal(t, uint64(1), ra.UpheldActions[reptypes.ActionKindForumCuration])
	require.Equal(t, uint64(0), ra.OverturnedActions[reptypes.ActionKindForumCuration])
	require.Equal(t, uint64(1), ra.ConsecutiveUpheld)

	// Three rejections on separate threads (per-thread cap stays clear) -> three
	// overturned ticks + demotion at DefaultMaxConsecutiveOverturnsBeforeDemotion.
	for i := 0; i < int(reptypes.DefaultMaxConsecutiveOverturnsBeforeDemotion); i++ {
		th := f.createTestPost(t, testCreator, 0, 0)
		rp := f.createTestPost(t, testCreator2, th.PostId, th.PostId)
		proposeReply(t, f, testSentinel, th.PostId, rp.PostId)
		_, err := ms.RejectProposedReply(f.ctx, &types.MsgRejectProposedReply{Creator: testCreator, ThreadId: th.PostId})
		require.NoError(t, err)
	}

	ra = f.repKeeper.roleActivities[testSentinel]
	require.Equal(t, uint64(1), ra.UpheldActions[reptypes.ActionKindForumCuration])
	require.Equal(t, uint64(reptypes.DefaultMaxConsecutiveOverturnsBeforeDemotion),
		ra.OverturnedActions[reptypes.ActionKindForumCuration])
	require.Equal(t, reptypes.DefaultMaxConsecutiveOverturnsBeforeDemotion, ra.ConsecutiveOverturns)
	require.Equal(t, uint64(0), ra.ConsecutiveUpheld)
	require.Equal(t, reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED,
		f.repKeeper.sentinels[testSentinel].BondStatus, "demoted after rejection streak")
}

// Phase C2: a sentinel may make at most max_accept_proposals_per_sentinel_per_thread
// proposals on a thread; the cap is per-sentinel, not thread-global.
func TestProposalCapPerSentinel(t *testing.T) {
	f := initFixture(t)
	f.ctx = f.sdkCtx().WithBlockTime(time.Unix(1_000_000, 0))
	f.createTestSentinel(t, testSentinel, "1000000000")
	f.createTestSentinel(t, testCreator2, "1000000000") // second sentinel
	thread := f.createTestPost(t, testCreator, 0, 0)
	reply := f.createTestPost(t, testCreator, thread.PostId, thread.PostId)
	ms := keeper.NewMsgServerImpl(f.keeper)

	cap := int(types.DefaultMaxAcceptProposalsPerSentinelPerThread)
	for i := 0; i < cap; i++ {
		proposeReply(t, f, testSentinel, thread.PostId, reply.PostId)
		_, err := ms.RejectProposedReply(f.ctx, &types.MsgRejectProposedReply{Creator: testCreator, ThreadId: thread.PostId})
		require.NoError(t, err)
	}
	// One past the cap is rejected.
	_, err := ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{Creator: testSentinel, ThreadId: thread.PostId, ReplyId: reply.PostId})
	require.ErrorIs(t, err, types.ErrMaxProposalsReached)

	// A different sentinel still has its own quota on the same thread.
	md := proposeReply(t, f, testCreator2, thread.PostId, reply.PostId)
	require.Equal(t, testCreator2, md.ProposedBy)
}

// Phase C3: the author can lock a thread against proposals; locking supersedes a
// pending proposal; only the author may toggle; unlock restores proposing.
func TestThreadProposalsLock(t *testing.T) {
	f := initFixture(t)
	now := int64(1_000_000)
	f.ctx = f.sdkCtx().WithBlockTime(time.Unix(now, 0))
	f.createTestSentinel(t, testSentinel, "1000000000")
	thread := f.createTestPost(t, testCreator, 0, 0)
	reply := f.createTestPost(t, testCreator2, thread.PostId, thread.PostId)
	ms := keeper.NewMsgServerImpl(f.keeper)

	// Non-author cannot lock.
	_, err := ms.SetThreadProposalsLock(f.ctx, &types.MsgSetThreadProposalsLock{Creator: testCreator2, ThreadId: thread.PostId, Locked: true})
	require.ErrorIs(t, err, types.ErrNotThreadAuthor)

	// Locking supersedes a pending proposal.
	md := proposeReply(t, f, testSentinel, thread.PostId, reply.PostId)
	_, err = ms.SetThreadProposalsLock(f.ctx, &types.MsgSetThreadProposalsLock{Creator: testCreator, ThreadId: thread.PostId, Locked: true})
	require.NoError(t, err)
	locked, err := f.keeper.ThreadMetadata.Get(f.ctx, thread.PostId)
	require.NoError(t, err)
	require.True(t, locked.ProposalsLocked)
	require.Equal(t, uint64(0), locked.ProposedReplyId, "lock superseded pending proposal")
	has, err := f.keeper.ProposalAutoConfirmQueue.Has(f.ctx, collections.Join(md.ProposalFireAt, thread.PostId))
	require.NoError(t, err)
	require.False(t, has)

	// Sentinel proposal blocked while locked.
	_, err = ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{Creator: testSentinel, ThreadId: thread.PostId, ReplyId: reply.PostId})
	require.ErrorIs(t, err, types.ErrThreadProposalsLocked)

	// Unlock restores proposing.
	_, err = ms.SetThreadProposalsLock(f.ctx, &types.MsgSetThreadProposalsLock{Creator: testCreator, ThreadId: thread.PostId, Locked: false})
	require.NoError(t, err)
	md2 := proposeReply(t, f, testSentinel, thread.PostId, reply.PostId)
	require.Equal(t, reply.PostId, md2.ProposedReplyId)
}

// The per-sentinel-per-thread proposal counter round-trips through genesis.
func TestProposalCountGenesisRoundTrip(t *testing.T) {
	f := initFixture(t)
	require.NoError(t, f.keeper.ProposalCountByThreadSentinel.Set(f.ctx, collections.Join(uint64(7), testSentinel), uint64(2)))
	require.NoError(t, f.keeper.ProposalCountByThreadSentinel.Set(f.ctx, collections.Join(uint64(8), testCreator), uint64(1)))

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, exported.ProposalCountMap, 2)

	f2 := initFixture(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, *exported))
	c, err := f2.keeper.ProposalCountByThreadSentinel.Get(f2.ctx, collections.Join(uint64(7), testSentinel))
	require.NoError(t, err)
	require.Equal(t, uint64(2), c)
}
