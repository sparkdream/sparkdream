package keeper_test

import (
	"errors"
	"testing"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// errInjectedMint is returned by the mock MintDREAM to exercise reward-failure paths.
var errInjectedMint = errors.New("injected mint failure")

// proposeReply has a bonded sentinel propose `reply` as the accepted answer on
// `thread`, returning the resulting metadata.
func proposeReply(t *testing.T, f *fixture, sentinel string, threadID, replyID uint64) types.ThreadMetadata {
	t.Helper()
	ms := keeper.NewMsgServerImpl(f.keeper)
	_, err := ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{
		Creator:  sentinel,
		ThreadId: threadID,
		ReplyId:  replyID,
	})
	require.NoError(t, err)
	md, err := f.keeper.ThreadMetadata.Get(f.ctx, threadID)
	require.NoError(t, err)
	return md
}

func TestSentinelProposeAcceptedReply(t *testing.T) {
	t.Run("sentinel proposal creates pending proposal + queue entry", func(t *testing.T) {
		f := initFixture(t)
		now := int64(1_000_000)
		f.ctx = f.sdkCtx().WithBlockTime(time.Unix(now, 0))

		f.createTestSentinel(t, testSentinel, "1000000000")
		thread := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator2, thread.PostId, thread.PostId)

		md := proposeReply(t, f, testSentinel, thread.PostId, reply.PostId)
		require.Equal(t, reply.PostId, md.ProposedReplyId)
		require.Equal(t, testSentinel, md.ProposedBy)
		require.Equal(t, uint64(0), md.AcceptedReplyId)

		// total_proposals incremented; the curation action kind NOT (only on
		// confirm — recorded on rep's shared RoleActivity).
		act, err := f.keeper.SentinelActivity.Get(f.ctx, testSentinel)
		require.NoError(t, err)
		require.Equal(t, uint64(1), act.TotalProposals)
		require.Equal(t, uint64(0),
			f.repKeeper.roleActivities[testSentinel].EpochActions[reptypes.ActionKindForumCuration])

		// Queue entry exists at the stamped fire_at.
		require.NotZero(t, md.ProposalFireAt)
		has, err := f.keeper.ProposalAutoConfirmQueue.Has(f.ctx, collections.Join(md.ProposalFireAt, thread.PostId))
		require.NoError(t, err)
		require.True(t, has)
	})

	t.Run("non-sentinel cannot propose", func(t *testing.T) {
		f := initFixture(t)
		f.ctx = f.sdkCtx().WithBlockTime(time.Unix(1_000_000, 0))
		thread := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator2, thread.PostId, thread.PostId)

		ms := keeper.NewMsgServerImpl(f.keeper)
		_, err := ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{
			Creator:  testCreator2,
			ThreadId: thread.PostId,
			ReplyId:  reply.PostId,
		})
		require.ErrorIs(t, err, types.ErrNotSentinel)
	})

	t.Run("demoted sentinel cannot propose", func(t *testing.T) {
		f := initFixture(t)
		f.ctx = f.sdkCtx().WithBlockTime(time.Unix(1_000_000, 0))
		f.createTestSentinel(t, testSentinel, "1000000000")
		br := f.repKeeper.sentinels[testSentinel]
		br.BondStatus = reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED
		f.repKeeper.sentinels[testSentinel] = br

		thread := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator2, thread.PostId, thread.PostId)

		ms := keeper.NewMsgServerImpl(f.keeper)
		_, err := ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{
			Creator:  testSentinel,
			ThreadId: thread.PostId,
			ReplyId:  reply.PostId,
		})
		require.ErrorIs(t, err, types.ErrSentinelDemoted)
	})

	t.Run("sentinel cannot clear accepted reply", func(t *testing.T) {
		f := initFixture(t)
		f.ctx = f.sdkCtx().WithBlockTime(time.Unix(1_000_000, 0))
		f.createTestSentinel(t, testSentinel, "1000000000")
		thread := f.createTestPost(t, testCreator, 0, 0)

		ms := keeper.NewMsgServerImpl(f.keeper)
		_, err := ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{
			Creator:  testSentinel,
			ThreadId: thread.PostId,
			ReplyId:  0,
		})
		require.ErrorIs(t, err, types.ErrSentinelCannotClearAccepted)
	})

	t.Run("cannot propose on bounty thread", func(t *testing.T) {
		f := initFixture(t)
		f.ctx = f.sdkCtx().WithBlockTime(time.Unix(1_000_000, 0))
		f.createTestSentinel(t, testSentinel, "1000000000")
		thread := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator2, thread.PostId, thread.PostId)
		// Mark the thread as having an active bounty.
		require.NoError(t, f.keeper.ActiveBountyByThread.Set(f.ctx, thread.PostId, 1))

		ms := keeper.NewMsgServerImpl(f.keeper)
		_, err := ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{
			Creator:  testSentinel,
			ThreadId: thread.PostId,
			ReplyId:  reply.PostId,
		})
		require.ErrorIs(t, err, types.ErrCannotMarkBountyThread)
	})

	t.Run("double proposal rejected", func(t *testing.T) {
		f := initFixture(t)
		f.ctx = f.sdkCtx().WithBlockTime(time.Unix(1_000_000, 0))
		f.createTestSentinel(t, testSentinel, "1000000000")
		thread := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator2, thread.PostId, thread.PostId)
		proposeReply(t, f, testSentinel, thread.PostId, reply.PostId)

		ms := keeper.NewMsgServerImpl(f.keeper)
		_, err := ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{
			Creator:  testSentinel,
			ThreadId: thread.PostId,
			ReplyId:  reply.PostId,
		})
		require.ErrorIs(t, err, types.ErrProposalAlreadyPending)
	})

	t.Run("cannot propose on members-restricted tag thread", func(t *testing.T) {
		f := initFixture(t)
		f.ctx = f.sdkCtx().WithBlockTime(time.Unix(1_000_000, 0))
		f.createTestSentinel(t, testSentinel, "1000000000")

		// Register a members-restricted reserved tag.
		if f.repKeeper.reservedTags == nil {
			f.repKeeper.reservedTags = make(map[string]reptypes.ReservedTag)
		}
		f.repKeeper.reservedTags["governance"] = reptypes.ReservedTag{
			Name:          "governance",
			MembersCanUse: false,
		}

		thread := f.createTestPost(t, testCreator, 0, 0)
		thread.Tags = []string{"governance"}
		require.NoError(t, f.keeper.Post.Set(f.ctx, thread.PostId, thread))
		reply := f.createTestPost(t, testCreator2, thread.PostId, thread.PostId)

		ms := keeper.NewMsgServerImpl(f.keeper)
		_, err := ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{
			Creator:  testSentinel,
			ThreadId: thread.PostId,
			ReplyId:  reply.PostId,
		})
		require.ErrorIs(t, err, types.ErrCannotMarkRestrictedTag)
	})
}

// TestProposalQueueStaleEntryNoOp verifies that after a manual confirm drains
// the queue, a later EndBlocker pass past the original fire_at is a clean no-op
// (no double-confirm, no extra reward).
func TestProposalQueueStaleEntryNoOp(t *testing.T) {
	f := initFixture(t)
	now := int64(5_000_000)
	f.ctx = f.sdkCtx().WithBlockTime(time.Unix(now, 0))
	f.createTestSentinel(t, testSentinel, "1000000000")
	thread := f.createTestPost(t, testCreator, 0, 0)
	reply := f.createTestPost(t, testCreator2, thread.PostId, thread.PostId)
	md := proposeReply(t, f, testSentinel, thread.PostId, reply.PostId)

	ms := keeper.NewMsgServerImpl(f.keeper)
	_, err := ms.ConfirmProposedReply(f.ctx, &types.MsgConfirmProposedReply{
		Creator:  testCreator,
		ThreadId: thread.PostId,
	})
	require.NoError(t, err)
	require.Len(t, f.repKeeper.mintDreamCalls, 1)

	// Queue already drained by confirm.
	has, err := f.keeper.ProposalAutoConfirmQueue.Has(f.ctx, collections.Join(md.ProposalFireAt, thread.PostId))
	require.NoError(t, err)
	require.False(t, has)

	// EndBlocker past the original fire_at must not re-confirm or re-reward.
	f.ctx = f.sdkCtx().WithBlockTime(time.Unix(md.ProposalFireAt+1, 0))
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	require.Len(t, f.repKeeper.mintDreamCalls, 1, "no second curation reward")

	act, err := f.keeper.SentinelActivity.Get(f.ctx, testSentinel)
	require.NoError(t, err)
	require.Equal(t, uint64(1), act.ConfirmedProposals)
	require.Equal(t, uint64(1),
		f.repKeeper.roleActivities[testSentinel].EpochActions[reptypes.ActionKindForumCuration])
}

// TestAutoConfirmRewardFailureFreesThread verifies that if the curation reward
// mint fails during EndBlocker auto-confirm, the thread is freed (proposal
// cleared) rather than wedged with a dangling pending proposal, and the queue
// entry is dropped.
func TestAutoConfirmRewardFailureFreesThread(t *testing.T) {
	f := initFixture(t)
	now := int64(6_000_000)
	f.ctx = f.sdkCtx().WithBlockTime(time.Unix(now, 0))
	f.createTestSentinel(t, testSentinel, "1000000000")
	thread := f.createTestPost(t, testCreator, 0, 0)
	reply := f.createTestPost(t, testCreator2, thread.PostId, thread.PostId)
	md := proposeReply(t, f, testSentinel, thread.PostId, reply.PostId)

	// Author active -> straight to auto-confirm (no extension); make the reward
	// mint fail so confirmCurationProposal errors.
	require.NoError(t, f.keeper.UserRateLimit.Set(f.ctx, testCreator, types.UserRateLimit{
		UserAddress:  testCreator,
		LastPostTime: now + 1,
	}))
	f.repKeeper.mintDreamErr = errInjectedMint

	f.ctx = f.sdkCtx().WithBlockTime(time.Unix(md.ProposalFireAt+1, 0))
	require.NoError(t, f.keeper.EndBlocker(f.ctx))

	// Thread freed: no pending proposal, not accepted, no reward, queue drained.
	updated, err := f.keeper.ThreadMetadata.Get(f.ctx, thread.PostId)
	require.NoError(t, err)
	require.Equal(t, uint64(0), updated.ProposedReplyId, "proposal cleared")
	require.Equal(t, uint64(0), updated.AcceptedReplyId, "not accepted")
	require.Equal(t, int64(0), updated.ProposalFireAt)
	require.Empty(t, f.repKeeper.mintDreamCalls)

	has, err := f.keeper.ProposalAutoConfirmQueue.Has(f.ctx, collections.Join(md.ProposalFireAt, thread.PostId))
	require.NoError(t, err)
	require.False(t, has, "queue entry dropped")

	// And the thread accepts a fresh proposal afterwards (not wedged).
	f.repKeeper.mintDreamErr = nil
	md2 := proposeReply(t, f, testSentinel, thread.PostId, reply.PostId)
	require.Equal(t, reply.PostId, md2.ProposedReplyId)
}

// TestProposalQueueGenesisRebuild verifies the derived auto-confirm queue is
// reconstructed from a pending proposal on InitGenesis (the queue is not a
// genesis field).
func TestProposalQueueGenesisRebuild(t *testing.T) {
	f := initFixture(t)
	fireAt := int64(9_000_000)
	gen := types.DefaultGenesis()
	gen.ThreadMetadataMap = []types.ThreadMetadata{
		{
			ThreadId:        7,
			ProposedReplyId: 42,
			ProposedBy:      testSentinel,
			ProposedAt:      fireAt - 100,
			ProposalFireAt:  fireAt,
		},
		// A thread with no pending proposal must NOT seed a queue entry.
		{ThreadId: 8},
	}
	require.NoError(t, f.keeper.InitGenesis(f.ctx, *gen))

	has, err := f.keeper.ProposalAutoConfirmQueue.Has(f.ctx, collections.Join(fireAt, uint64(7)))
	require.NoError(t, err)
	require.True(t, has, "pending-proposal queue entry rebuilt")

	// Count entries: exactly one.
	count := 0
	require.NoError(t, f.keeper.ProposalAutoConfirmQueue.Walk(f.ctx, nil, func(_ collections.Pair[int64, uint64]) (bool, error) {
		count++
		return false, nil
	}))
	require.Equal(t, 1, count)
}

func TestRejectProposedReplyCounter(t *testing.T) {
	f := initFixture(t)
	f.ctx = f.sdkCtx().WithBlockTime(time.Unix(1_000_000, 0))
	f.createTestSentinel(t, testSentinel, "1000000000")
	thread := f.createTestPost(t, testCreator, 0, 0)
	reply := f.createTestPost(t, testCreator2, thread.PostId, thread.PostId)
	md := proposeReply(t, f, testSentinel, thread.PostId, reply.PostId)

	ms := keeper.NewMsgServerImpl(f.keeper)
	_, err := ms.RejectProposedReply(f.ctx, &types.MsgRejectProposedReply{
		Creator:  testCreator,
		ThreadId: thread.PostId,
	})
	require.NoError(t, err)

	updated, err := f.keeper.ThreadMetadata.Get(f.ctx, thread.PostId)
	require.NoError(t, err)
	require.Equal(t, uint64(0), updated.ProposedReplyId)
	require.Equal(t, uint64(0), updated.AcceptedReplyId)

	act, err := f.keeper.SentinelActivity.Get(f.ctx, testSentinel)
	require.NoError(t, err)
	require.Equal(t, uint64(1), act.RejectedProposals)
	require.Equal(t, uint64(0), act.ConfirmedProposals)

	// Queue entry removed.
	has, err := f.keeper.ProposalAutoConfirmQueue.Has(f.ctx, collections.Join(md.ProposalFireAt, thread.PostId))
	require.NoError(t, err)
	require.False(t, has)
}

func TestConfirmProposedReplyReward(t *testing.T) {
	f := initFixture(t)
	f.ctx = f.sdkCtx().WithBlockTime(time.Unix(1_000_000, 0))
	f.createTestSentinel(t, testSentinel, "1000000000")
	thread := f.createTestPost(t, testCreator, 0, 0)
	reply := f.createTestPost(t, testCreator2, thread.PostId, thread.PostId)
	proposeReply(t, f, testSentinel, thread.PostId, reply.PostId)

	ms := keeper.NewMsgServerImpl(f.keeper)
	_, err := ms.ConfirmProposedReply(f.ctx, &types.MsgConfirmProposedReply{
		Creator:  testCreator,
		ThreadId: thread.PostId,
	})
	require.NoError(t, err)

	// Curation DREAM reward minted to the proposing sentinel.
	require.Len(t, f.repKeeper.mintDreamCalls, 1)
	require.Equal(t, testSentinel, f.repKeeper.mintDreamCalls[0].Addr)
	require.Equal(t, types.DefaultCurationDreamReward, f.repKeeper.mintDreamCalls[0].Amount)
}

// TestCurationRewardZeroDisables verifies that a curation_dream_reward of 0
// disables the mint (read directly, no default fallback), while the confirm
// itself still succeeds and the counters still increment.
func TestCurationRewardZeroDisables(t *testing.T) {
	f := initFixture(t)
	f.ctx = f.sdkCtx().WithBlockTime(time.Unix(1_000_000, 0))

	params := types.DefaultParams()
	params.CurationDreamReward = math.ZeroInt()
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	f.createTestSentinel(t, testSentinel, "1000000000")
	thread := f.createTestPost(t, testCreator, 0, 0)
	reply := f.createTestPost(t, testCreator2, thread.PostId, thread.PostId)
	proposeReply(t, f, testSentinel, thread.PostId, reply.PostId)

	ms := keeper.NewMsgServerImpl(f.keeper)
	_, err := ms.ConfirmProposedReply(f.ctx, &types.MsgConfirmProposedReply{
		Creator:  testCreator,
		ThreadId: thread.PostId,
	})
	require.NoError(t, err)

	// Confirm succeeded and counter incremented, but no reward minted.
	require.Empty(t, f.repKeeper.mintDreamCalls, "zero reward => no mint")
	act, err := f.keeper.SentinelActivity.Get(f.ctx, testSentinel)
	require.NoError(t, err)
	require.Equal(t, uint64(1), act.ConfirmedProposals)
}

func TestProposalAutoConfirm(t *testing.T) {
	t.Run("auto-confirms after timeout when author is active", func(t *testing.T) {
		f := initFixture(t)
		now := int64(2_000_000)
		f.ctx = f.sdkCtx().WithBlockTime(time.Unix(now, 0))
		f.createTestSentinel(t, testSentinel, "1000000000")
		thread := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator2, thread.PostId, thread.PostId)
		md := proposeReply(t, f, testSentinel, thread.PostId, reply.PostId)

		// Author was active AFTER the proposal — no extension, straight confirm.
		require.NoError(t, f.keeper.UserRateLimit.Set(f.ctx, testCreator, types.UserRateLimit{
			UserAddress:  testCreator,
			LastPostTime: now + 1,
		}))

		// Advance past the fire_at and run the EndBlocker.
		f.ctx = f.sdkCtx().WithBlockTime(time.Unix(md.ProposalFireAt+1, 0))
		require.NoError(t, f.keeper.EndBlocker(f.ctx))

		updated, err := f.keeper.ThreadMetadata.Get(f.ctx, thread.PostId)
		require.NoError(t, err)
		require.Equal(t, reply.PostId, updated.AcceptedReplyId)
		require.Equal(t, testSentinel, updated.AcceptedBy)
		require.Equal(t, uint64(0), updated.ProposedReplyId)

		act, err := f.keeper.SentinelActivity.Get(f.ctx, testSentinel)
		require.NoError(t, err)
		require.Equal(t, uint64(1), act.ConfirmedProposals)
		require.Equal(t, uint64(1),
			f.repKeeper.roleActivities[testSentinel].EpochActions[reptypes.ActionKindForumCuration])
		require.Len(t, f.repKeeper.mintDreamCalls, 1)
	})

	t.Run("extends once for inactive author, then confirms", func(t *testing.T) {
		f := initFixture(t)
		now := int64(3_000_000)
		f.ctx = f.sdkCtx().WithBlockTime(time.Unix(now, 0))
		f.createTestSentinel(t, testSentinel, "1000000000")
		thread := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator2, thread.PostId, thread.PostId)
		md := proposeReply(t, f, testSentinel, thread.PostId, reply.PostId)
		// Author has no UserRateLimit record -> last-active 0 -> appears inactive.

		// First EndBlocker past the timeout: grants the one-time extension.
		f.ctx = f.sdkCtx().WithBlockTime(time.Unix(md.ProposalFireAt+1, 0))
		require.NoError(t, f.keeper.EndBlocker(f.ctx))

		extended, err := f.keeper.ThreadMetadata.Get(f.ctx, thread.PostId)
		require.NoError(t, err)
		require.True(t, extended.ProposalExtended)
		require.Equal(t, uint64(0), extended.AcceptedReplyId) // not yet confirmed
		require.NotEqual(t, md.ProposalFireAt, extended.ProposalFireAt)
		require.Empty(t, f.repKeeper.mintDreamCalls)

		// Second EndBlocker past the new fire_at: confirms regardless of activity.
		f.ctx = f.sdkCtx().WithBlockTime(time.Unix(extended.ProposalFireAt+1, 0))
		require.NoError(t, f.keeper.EndBlocker(f.ctx))

		confirmed, err := f.keeper.ThreadMetadata.Get(f.ctx, thread.PostId)
		require.NoError(t, err)
		require.Equal(t, reply.PostId, confirmed.AcceptedReplyId)
		require.Len(t, f.repKeeper.mintDreamCalls, 1)
	})
}
