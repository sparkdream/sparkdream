package keeper_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// TestReplyPinAppealResolution exercises the REPLY_PIN branches of the forum
// adapter that x/rep's ResolveGovActionAppeal drives: sentinel/committed-amount
// resolution from a reply id, upheld (pin stays) and overturned (pin removed)
// counter updates.
func TestReplyPinAppealResolution(t *testing.T) {
	f := initFixture(t)

	const pinType = reptypes.GovActionType_GOV_ACTION_TYPE_REPLY_PIN
	target := func(id uint64) string { return strconv.FormatUint(id, 10) }

	// setup builds a thread with one sentinel-pinned reply (committed bond 1000)
	// and returns the thread + reply posts.
	setup := func(t *testing.T) (types.Post, types.Post) {
		t.Helper()
		thread := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator2, thread.PostId, 0)
		now := f.sdkCtx().BlockTime().Unix()
		metadata := types.ThreadMetadata{
			ThreadId:       thread.PostId,
			PinnedReplyIds: []uint64{reply.PostId},
			PinnedRecords: []*types.PinnedReplyRecord{
				{
					PostId:          reply.PostId,
					PinnedBy:        testSentinel,
					PinnedAt:        now,
					IsSentinelPin:   true,
					CommittedAmount: "1000",
				},
			},
		}
		require.NoError(t, f.keeper.ThreadMetadata.Set(f.ctx, thread.PostId, metadata))
		return thread, reply
	}

	t.Run("sentinel and committed amount resolve from reply id", func(t *testing.T) {
		_, reply := setup(t)

		s, err := f.keeper.GetActionSentinel(f.ctx, pinType, target(reply.PostId))
		require.NoError(t, err)
		require.Equal(t, testSentinel, s)

		amt, err := f.keeper.GetActionCommittedAmount(f.ctx, pinType, target(reply.PostId))
		require.NoError(t, err)
		require.Equal(t, "1000", amt.String())
	})

	t.Run("missing record is a soft skip", func(t *testing.T) {
		s, err := f.keeper.GetActionSentinel(f.ctx, pinType, target(999999))
		require.NoError(t, err)
		require.Equal(t, "", s)

		amt, err := f.keeper.GetActionCommittedAmount(f.ctx, pinType, target(999999))
		require.NoError(t, err)
		require.True(t, amt.IsZero())
	})

	t.Run("resolution hook is a no-op for pins (no pending count) and keeps the pin", func(t *testing.T) {
		// Verdict counters/streaks live on rep's RoleActivity now (recorded by
		// x/rep's appeal resolver, not the forum adapter). Forum's remaining
		// resolution bookkeeping — the pending-hide decrement — does not apply
		// to pins, so the hook is a pure no-op here.
		thread, _ := setup(t)

		require.NoError(t, f.keeper.OnSentinelActionResolved(f.ctx, pinType, target(thread.PostId)))

		md, err := f.keeper.ThreadMetadata.Get(f.ctx, thread.PostId)
		require.NoError(t, err)
		require.Len(t, md.PinnedRecords, 1)
		require.Len(t, md.PinnedReplyIds, 1)
	})

	t.Run("overturned pin is removed by ReverseSentinelAction", func(t *testing.T) {
		thread, reply := setup(t)

		// ReverseSentinelAction removes the pin from both lists.
		require.NoError(t, f.keeper.ReverseSentinelAction(f.ctx, pinType, target(reply.PostId)))

		md, err := f.keeper.ThreadMetadata.Get(f.ctx, thread.PostId)
		require.NoError(t, err)
		require.Empty(t, md.PinnedRecords)
		require.Empty(t, md.PinnedReplyIds)
	})
}
