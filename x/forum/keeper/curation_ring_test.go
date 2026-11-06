package keeper_test

import (
	"context"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// FRM-101 regression: a sentinel and an account it controls must not be able
// to self-deal accepted-reply curations. A confirmation by a thread author who
// is not an active member, or who is the sentinel's invitee or inviter, still
// accepts the reply but mints nothing and records no verdict.
func TestCurationConfirmedByAffiliatedAuthorIsNotRewarded(t *testing.T) {
	for _, tc := range []struct {
		name     string
		nonMemb  bool
		invited  bool // thread author was invited by the sentinel
		inviter  bool // thread author invited the sentinel
		rewarded bool
	}{
		{name: "independent member", rewarded: true},
		{name: "non-member", nonMemb: true},
		{name: "sentinel's invitee", invited: true},
		{name: "sentinel's inviter", inviter: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			f.ctx = f.sdkCtx().WithBlockTime(time.Unix(1_000_000, 0))
			f.createTestSentinel(t, testSentinel, "1000000000")
			thread := f.createTestPost(t, testCreator, 0, 0)
			reply := f.createTestPost(t, testCreator2, thread.PostId, thread.PostId)

			f.repKeeper.IsActiveMemberFn = func(_ context.Context, addr sdk.AccAddress) bool {
				return !(tc.nonMemb && addr.String() == testCreator)
			}
			f.repKeeper.GetMemberFn = func(_ context.Context, addr sdk.AccAddress) (reptypes.Member, error) {
				m := reptypes.Member{Address: addr.String(), TrustLevel: reptypes.TrustLevel_TRUST_LEVEL_TRUSTED}
				if tc.invited && addr.String() == testCreator {
					m.InvitedBy = testSentinel
				}
				if tc.inviter && addr.String() == testSentinel {
					m.InvitedBy = testCreator
				}
				return m, nil
			}
			// Start from an overturn streak the ring would like to clear.
			ra := f.repKeeper.roleActivity(testSentinel)
			ra.ConsecutiveOverturns = 2
			f.repKeeper.roleActivities[testSentinel] = ra

			proposeReply(t, f, testSentinel, thread.PostId, reply.PostId)
			ms := keeper.NewMsgServerImpl(f.keeper)
			_, err := ms.ConfirmProposedReply(f.ctx, &types.MsgConfirmProposedReply{Creator: testCreator, ThreadId: thread.PostId})
			require.NoError(t, err)

			md, err := f.keeper.ThreadMetadata.Get(f.ctx, thread.PostId)
			require.NoError(t, err)
			require.Equal(t, reply.PostId, md.AcceptedReplyId, "the reply is accepted either way")

			ra = f.repKeeper.roleActivities[testSentinel]
			require.Equal(t, uint64(2), ra.ConsecutiveOverturns, "a curation never clears the overturn streak")
			if tc.rewarded {
				require.Len(t, f.repKeeper.mintDreamCalls, 1)
				require.Equal(t, uint64(1), ra.UpheldActions[reptypes.ActionKindForumCuration])
				return
			}
			require.Empty(t, f.repKeeper.mintDreamCalls, "no curation reward")
			require.Zero(t, ra.UpheldActions[reptypes.ActionKindForumCuration], "no upheld verdict")
			require.Zero(t, ra.EpochActions[reptypes.ActionKindForumCuration], "no curation action")
		})
	}
}

// FRM-201 regression: a sentinel cannot propose a reply the thread author
// wrote. One arm's-length accomplice could otherwise post a thread and its
// answer and confirm the sentinel's proposal, farming the curation reward and
// the accuracy ring. The author may still accept their own reply directly
// (no reward, no verdict), and two anonymous posts are not the same member.
func TestSentinelCannotProposeThreadAuthorsOwnReply(t *testing.T) {
	f := initFixture(t)
	f.createTestSentinel(t, testSentinel, "1000000000")
	ms := keeper.NewMsgServerImpl(f.keeper)

	thread := f.createTestPost(t, testCreator, 0, 0)
	selfReply := f.createTestPost(t, testCreator, thread.PostId, thread.PostId)
	otherReply := f.createTestPost(t, testCreator2, thread.PostId, thread.PostId)

	_, err := ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{Creator: testSentinel, ThreadId: thread.PostId, ReplyId: selfReply.PostId})
	require.ErrorIs(t, err, types.ErrCannotCurateSelfAnswer)
	md, err := f.keeper.ThreadMetadata.Get(f.ctx, thread.PostId)
	require.True(t, err != nil || md.ProposedReplyId == 0, "no proposal recorded")
	require.Empty(t, f.repKeeper.mintDreamCalls)

	// The author accepting their own reply is not a curation and stays allowed.
	_, err = ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{Creator: testCreator, ThreadId: thread.PostId, ReplyId: selfReply.PostId})
	require.NoError(t, err)
	_, err = ms.MarkAcceptedReply(f.ctx, &types.MsgMarkAcceptedReply{Creator: testCreator, ThreadId: thread.PostId, ReplyId: 0})
	require.NoError(t, err, "cleared again")

	// Someone else's reply can be proposed.
	proposeReply(t, f, testSentinel, thread.PostId, otherReply.PostId)
}
