package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
	reptypes "sparkdream/x/rep/types"
)

// challengeTimeoutSetup rates a collection as the curator (f.member) and
// returns the review ID plus a member challenger that is not the curator.
func challengeTimeoutSetup(t *testing.T, f *testFixture) (uint64, uint64, sdk.AccAddress, string) {
	t.Helper()
	f.setBlockHeight(100)
	collID := f.createCollection(t, f.owner)
	f.registerCurator(t, f.member, 500_000_000)
	f.advanceBlockHeight(14401)
	resp, err := f.msgServer.RateCollection(f.ctx, &types.MsgRateCollection{
		Creator:      f.member,
		CollectionId: collID,
		Verdict:      types.CurationVerdict_CURATION_VERDICT_UP,
	})
	require.NoError(t, err)

	challengerAddr := sdk.AccAddress([]byte("challenger__________"))
	challengerStr, err := f.addressCodec.BytesToString(challengerAddr)
	require.NoError(t, err)
	orig := f.repKeeper.isMemberFn
	f.repKeeper.isMemberFn = func(ctx context.Context, addr sdk.AccAddress) bool {
		if addr.Equals(challengerAddr) {
			return true
		}
		if orig != nil {
			return orig(ctx, addr)
		}
		return true
	}
	f.repKeeper.unlockDREAMFn = func(_ context.Context, addr sdk.AccAddress, amount math.Int) error {
		f.repKeeper.unlockCalls = append(f.repKeeper.unlockCalls, dreamCall{addr: addr, amount: amount})
		return nil
	}
	f.repKeeper.burnDREAMFn = func(_ context.Context, addr sdk.AccAddress, amount math.Int) error {
		f.repKeeper.burnCalls = append(f.repKeeper.burnCalls, dreamCall{addr: addr, amount: amount})
		return nil
	}
	return collID, resp.ReviewId, challengerAddr, challengerStr
}

func curatorCommitted(t *testing.T, f *testFixture) math.Int {
	t.Helper()
	br, err := f.repKeeper.GetBondedRole(f.ctx, reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR, f.member)
	require.NoError(t, err)
	committed, ok := math.NewIntFromString(br.TotalCommittedBond)
	if !ok {
		return math.ZeroInt()
	}
	return committed
}

func hasEvent(f *testFixture, eventType string) bool {
	for _, ev := range f.sdkCtx.EventManager().Events() {
		if ev.Type == eventType {
			return true
		}
	}
	return false
}

// A challenge nobody adjudicates is resolved at its deadline: the curator's
// reservation is released, the challenger's deposit returned, and the review
// left challenged with no payable challenger.
func TestChallengeTimeout_ResolvesUnadjudicatedChallenge(t *testing.T) {
	f := initTestFixture(t)
	collID, reviewID, challengerAddr, challengerStr := challengeTimeoutSetup(t, f)

	challengedAt := f.sdkCtx.BlockHeight()
	_, err := f.msgServer.ChallengeReview(f.ctx, &types.MsgChallengeReview{
		Creator:  challengerStr,
		ReviewId: reviewID,
		Reason:   "inaccurate review",
	})
	require.NoError(t, err)
	require.True(t, curatorCommitted(t, f).IsPositive())

	deadline := challengedAt + types.ChallengeResolutionTimeoutBlocks
	has, err := f.keeper.CurationChallengeDeadline.Has(f.ctx, collections.Join(deadline, reviewID))
	require.NoError(t, err)
	require.True(t, has, "MsgChallengeReview schedules the timeout")

	// One block early: still pending.
	f.setBlockHeight(deadline - 1)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	review, err := f.keeper.CurationReview.Get(f.ctx, reviewID)
	require.NoError(t, err)
	require.Equal(t, challengerStr, review.Challenger)
	require.Empty(t, f.repKeeper.unlockCalls)

	// At the deadline: resolved as not adjudicated.
	f.setBlockHeight(deadline)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))

	review, err = f.keeper.CurationReview.Get(f.ctx, reviewID)
	require.NoError(t, err)
	require.True(t, review.Challenged, "a timed-out review cannot be re-challenged")
	require.False(t, review.Overturned)
	require.Empty(t, review.Challenger)
	require.True(t, review.CommittedSlash.IsZero())
	require.True(t, curatorCommitted(t, f).IsZero(), "curator reservation released")
	require.Equal(t, []dreamCall{{addr: challengerAddr, amount: types.DefaultChallengeDeposit}}, f.repKeeper.unlockCalls)
	require.Empty(t, f.repKeeper.burnCalls)
	require.Empty(t, f.repKeeper.roleOutcomeCalls, "no verdict, no curator outcome")
	require.True(t, hasEvent(f, "challenge_timed_out"))

	has, err = f.keeper.CurationChallengeDeadline.Has(f.ctx, collections.Join(deadline, reviewID))
	require.NoError(t, err)
	require.False(t, has)

	// A late verdict cannot settle the challenge a second time.
	require.Error(t, f.keeper.ResolveChallengeResult(f.ctx, reviewID, false))
	require.Empty(t, f.repKeeper.burnCalls)

	// Nor does deleting the collection refund the deposit again.
	_, err = f.msgServer.DeleteCollection(f.ctx, &types.MsgDeleteCollection{Creator: f.owner, Id: collID})
	require.NoError(t, err)
	require.Len(t, f.repKeeper.unlockCalls, 1)
}

// A challenge resolved by verdict before its deadline leaves a stale index
// entry that the sweep drops without paying anything.
func TestChallengeTimeout_SkipsResolvedChallenge(t *testing.T) {
	f := initTestFixture(t)
	_, reviewID, _, challengerStr := challengeTimeoutSetup(t, f)

	_, err := f.msgServer.ChallengeReview(f.ctx, &types.MsgChallengeReview{
		Creator: challengerStr, ReviewId: reviewID, Reason: "inaccurate review",
	})
	require.NoError(t, err)
	require.NoError(t, f.keeper.ResolveChallengeResult(f.ctx, reviewID, false))
	require.Len(t, f.repKeeper.burnCalls, 1)
	// The rejected challenge unlocks the deposit right before burning it.
	require.Len(t, f.repKeeper.unlockCalls, 1)

	f.advanceBlockHeight(types.ChallengeResolutionTimeoutBlocks)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	require.Len(t, f.repKeeper.unlockCalls, 1, "burned deposit is not refunded by the timeout")
	require.False(t, hasEvent(f, "challenge_timed_out"))

	empty := true
	require.NoError(t, f.keeper.CurationChallengeDeadline.Walk(f.ctx, nil, func(collections.Pair[int64, uint64]) (bool, error) {
		empty = false
		return true, nil
	}))
	require.True(t, empty)
}

// Challenges opened before the deadline index existed are found by the
// backfill and time out at review creation + challenge window + timeout.
func TestChallengeTimeout_BackfillsLegacyChallenge(t *testing.T) {
	f := initTestFixture(t)
	_, reviewID, challengerAddr, challengerStr := challengeTimeoutSetup(t, f)

	// Simulate an unindexed challenge (as genesis import leaves one): reservation + review state, no index
	// entry, no backfill cursor.
	slash := math.NewInt(50_000_000)
	require.NoError(t, f.repKeeper.ReserveBond(f.ctx, reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR, f.member, slash))
	review, err := f.keeper.CurationReview.Get(f.ctx, reviewID)
	require.NoError(t, err)
	review.Challenged = true
	review.Challenger = challengerStr
	review.CommittedSlash = slash
	require.NoError(t, f.keeper.CurationReview.Set(f.ctx, reviewID, review))

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	deadline := review.CreatedAt + params.ChallengeWindowBlocks + types.ChallengeResolutionTimeoutBlocks

	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	has, err := f.keeper.CurationChallengeDeadline.Has(f.ctx, collections.Join(deadline, reviewID))
	require.NoError(t, err)
	require.True(t, has, "backfill indexed the legacy challenge")
	cursor, err := f.keeper.CurationChallengeBackfill.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeBackfillDone, cursor)

	f.setBlockHeight(deadline)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	review, err = f.keeper.CurationReview.Get(f.ctx, reviewID)
	require.NoError(t, err)
	require.Empty(t, review.Challenger)
	require.True(t, curatorCommitted(t, f).IsZero())
	require.Equal(t, []dreamCall{{addr: challengerAddr, amount: types.DefaultChallengeDeposit}}, f.repKeeper.unlockCalls)
}

// The backfill reads at most ChallengeBackfillScanPerBlock reviews per block
// and resumes from its cursor.
func TestChallengeTimeout_BackfillIsCapped(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(10)

	total := types.ChallengeBackfillScanPerBlock + 20
	for i := 1; i <= total; i++ {
		require.NoError(t, f.keeper.CurationReview.Set(f.ctx, uint64(i), types.CurationReview{
			Id:             uint64(i),
			CollectionId:   1,
			Curator:        f.member,
			CreatedAt:      1,
			Challenged:     true,
			Challenger:     f.owner,
			CommittedSlash: math.ZeroInt(),
		}))
	}

	countIndexed := func() int {
		n := 0
		require.NoError(t, f.keeper.CurationChallengeDeadline.Walk(f.ctx, nil, func(collections.Pair[int64, uint64]) (bool, error) {
			n++
			return false, nil
		}))
		return n
	}

	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	require.Equal(t, types.ChallengeBackfillScanPerBlock, countIndexed())
	cursor, err := f.keeper.CurationChallengeBackfill.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(types.ChallengeBackfillScanPerBlock+1), cursor)

	f.advanceBlockHeight(1)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	require.Equal(t, total, countIndexed())
	cursor, err = f.keeper.CurationChallengeBackfill.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeBackfillDone, cursor)
}
