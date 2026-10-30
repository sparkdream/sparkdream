package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// dueCount returns how many entries of q are due at or before height.
func dueCount(t *testing.T, f *fixture, q collections.KeySet[collections.Pair[int64, uint64]], height int64) int {
	t.Helper()
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join[int64, uint64](height+1, 0))
	n := 0
	require.NoError(t, q.Walk(f.ctx, rng, func(collections.Pair[int64, uint64]) (bool, error) {
		n++
		return false, nil
	}))
	return n
}

// TestREPE10_DueSweepIsCappedPerBlock: the content-challenge auto-uphold
// sweep reads at most 100 due entries per block. 150 stale entries sit ahead
// of one real overdue challenge: the first block drops 100 of them and leaves
// the challenge ACTIVE, the next block drops the rest and upholds it.
func TestREPE10_DueSweepIsCappedPerBlock(t *testing.T) {
	f, _, challengerAddr := setupContentChallengeFixture(t)
	k := f.keeper

	ccID, err := k.CreateContentChallenge(f.ctx, challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 1,
		"Inaccurate claims", []string{"evidence"}, math.NewInt(100_000_000))
	require.NoError(t, err)
	cc, err := k.ContentChallenge.Get(f.ctx, ccID)
	require.NoError(t, err)
	require.True(t, cc.ResponseDeadline > 0)
	has, err := k.ContentChallengeResponseDue.Has(f.ctx, collections.Join(cc.ResponseDeadline, ccID))
	require.NoError(t, err)
	require.True(t, has, "creating the challenge queues it at its response deadline")

	// Stale hints for records that do not exist, all due before the real one.
	for i := uint64(0); i < 150; i++ {
		require.NoError(t, k.ContentChallengeResponseDue.Set(f.ctx, collections.Join(int64(0), 1_000_000+i)))
	}

	f.ctx = f.ctx.WithBlockHeight(cc.ResponseDeadline + 1)
	require.Equal(t, 151, dueCount(t, f, k.ContentChallengeResponseDue, f.ctx.BlockHeight()))

	require.NoError(t, k.EndBlocker(f.ctx))
	require.Equal(t, 51, dueCount(t, f, k.ContentChallengeResponseDue, f.ctx.BlockHeight()),
		"one block reads at most 100 entries")
	cc, err = k.ContentChallenge.Get(f.ctx, ccID)
	require.NoError(t, err)
	require.Equal(t, types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_ACTIVE, cc.Status,
		"the challenge past the cap waits for the next block")

	f.ctx = f.ctx.WithBlockHeight(f.ctx.BlockHeight() + 1)
	require.NoError(t, k.EndBlocker(f.ctx))
	require.Zero(t, dueCount(t, f, k.ContentChallengeResponseDue, f.ctx.BlockHeight()))
	cc, err = k.ContentChallenge.Get(f.ctx, ccID)
	require.NoError(t, err)
	require.Equal(t, types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_UPHELD, cc.Status)
}

// TestREPE10_DueEntryRequeuedAtDeadline: an entry that comes due before its
// item's deadline (written at 0 by a status change) is moved to the deadline
// rather than processed early or dropped.
func TestREPE10_DueEntryRequeuedAtDeadline(t *testing.T) {
	f, _, challengerAddr := setupContentChallengeFixture(t)
	k := f.keeper
	ccID, err := k.CreateContentChallenge(f.ctx, challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 1,
		"Inaccurate claims", []string{"evidence"}, math.NewInt(100_000_000))
	require.NoError(t, err)
	cc, err := k.ContentChallenge.Get(f.ctx, ccID)
	require.NoError(t, err)
	require.NoError(t, k.ContentChallengeResponseDue.Remove(f.ctx, collections.Join(cc.ResponseDeadline, ccID)))
	require.NoError(t, k.ContentChallengeResponseDue.Set(f.ctx, collections.Join(int64(0), ccID)))

	require.NoError(t, k.EndBlocker(f.ctx))
	has, err := k.ContentChallengeResponseDue.Has(f.ctx, collections.Join(cc.ResponseDeadline, ccID))
	require.NoError(t, err)
	require.True(t, has, "re-queued at the real deadline")
	cc, err = k.ContentChallenge.Get(f.ctx, ccID)
	require.NoError(t, err)
	require.Equal(t, types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_ACTIVE, cc.Status)
}

// TestREPE10_DueQueuesRebuiltAtGenesis: the due queues are not exported; an
// import rebuilds them from the live records.
func TestREPE10_DueQueuesRebuiltAtGenesis(t *testing.T) {
	f, _, challengerAddr := setupContentChallengeFixture(t)
	k := f.keeper
	ccID, err := k.CreateContentChallenge(f.ctx, challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 1,
		"Inaccurate claims", []string{"evidence"}, math.NewInt(100_000_000))
	require.NoError(t, err)
	cc, err := k.ContentChallenge.Get(f.ctx, ccID)
	require.NoError(t, err)

	gen, err := k.ExportGenesis(f.ctx)
	require.NoError(t, err)
	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *gen))
	has, err := g.keeper.ContentChallengeResponseDue.Has(g.ctx, collections.Join(cc.ResponseDeadline, ccID))
	require.NoError(t, err)
	require.True(t, has)
}

// TestREPE10_SubmittedCheckIsCappedPerBlock: the completion-threshold check
// visits at most 100 SUBMITTED initiatives per block, resumes after the last
// one it visited, and starts over once it reaches the end. Index entries
// without a record make every check fail fast, which is enough to watch the
// cursor.
func TestREPE10_SubmittedCheckIsCappedPerBlock(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	submitted := int32(types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED)
	for id := uint64(1); id <= 150; id++ {
		require.NoError(t, k.InitiativesByStatus.Set(f.ctx, collections.Join(submitted, id)))
	}
	// An initiative in another status is never visited.
	require.NoError(t, k.InitiativesByStatus.Set(f.ctx,
		collections.Join(int32(types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW), uint64(500))))

	k.CheckSubmittedInitiatives(f.ctx)
	cursor, err := k.SubmittedCheckCursor.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(100), cursor, "first block stops after 100 checks")

	k.CheckSubmittedInitiatives(f.ctx)
	cursor, err = k.SubmittedCheckCursor.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(0), cursor, "second block checks the last 50 and wraps")

	k.CheckSubmittedInitiatives(f.ctx)
	cursor, err = k.SubmittedCheckCursor.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(100), cursor, "third block starts over from the lowest id")
}
