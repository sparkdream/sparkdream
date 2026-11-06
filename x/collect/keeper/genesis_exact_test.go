package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// TestGenesisRebuildMatchesLiveIndexes covers the indexes InitGenesis rebuilds
// for hide records and curation challenges, each matching what the live write
// paths leave behind:
//   - an appealed hide has no expiry entry (MsgAppealHide drops it);
//   - a hide on a deleted collection or item has no by-target entry
//     (deleteCollectionFull / cleanupItemHideRecords drop them, the record
//     stays);
//   - a challenge timeout keeps its exported deadline, a pending challenge
//     without one gets the latest possible deadline, and the backfill is done.
func TestGenesisRebuildMatchesLiveIndexes(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	params := types.DefaultParams()

	gs := types.DefaultGenesis()
	gs.Collections = []types.Collection{{Id: 1, Owner: "phoenix"}}
	gs.CollectionCount = 2
	gs.Items = []types.Item{{Id: 6, CollectionId: 1, AddedBy: "phoenix"}}
	gs.ItemCount = 7
	gs.HideRecords = []types.HideRecord{
		{Id: 1, TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_ITEM, TargetId: 5, AppealDeadline: 100},
		{Id: 2, TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_ITEM, TargetId: 6, AppealDeadline: 200, Appealed: true},
		{Id: 3, TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, TargetId: 9, AppealDeadline: 300, Resolved: true},
		{Id: 4, TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, TargetId: 1, AppealDeadline: 400, Resolved: true},
	}
	gs.HideRecordCount = 5
	gs.CurationReviews = []types.CurationReview{
		{Id: 1, CollectionId: 1, Curator: "aurora", CreatedAt: 10, Challenged: true, Challenger: "zenith"},
		{Id: 2, CollectionId: 1, Curator: "aurora", CreatedAt: 20, Challenged: true, Challenger: "zenith"},
	}
	gs.CurationReviewCount = 3
	gs.CurationChallengeDeadlines = []types.CurationChallengeDeadline{{Deadline: 777, ReviewId: 1}}
	require.NoError(t, k.InitGenesis(f.ctx, *gs))

	hasExpiry := func(deadline int64, id uint64) bool {
		ok, err := k.HideRecordExpiry.Has(f.ctx, collections.Join(deadline, id))
		require.NoError(t, err)
		return ok
	}
	require.True(t, hasExpiry(100, 1), "open unappealed hide keeps its expiry")
	require.False(t, hasExpiry(200, 2), "appealed hide is owned by the x/rep appeal")

	hasTarget := func(tt types.FlagTargetType, target, id uint64) bool {
		ok, err := k.HideRecordByTarget.Has(f.ctx, collections.Join(keeper.HideRecordTargetCompositeKey(tt, target), id))
		require.NoError(t, err)
		return ok
	}
	require.False(t, hasTarget(types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, 9, 3), "deleted collection")
	require.True(t, hasTarget(types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, 1, 4))
	require.True(t, hasTarget(types.FlagTargetType_FLAG_TARGET_TYPE_ITEM, 6, 2))
	require.False(t, hasTarget(types.FlagTargetType_FLAG_TARGET_TYPE_ITEM, 5, 1), "deleted item")

	hasDeadline := func(deadline int64, id uint64) bool {
		ok, err := k.CurationChallengeDeadline.Has(f.ctx, collections.Join(deadline, id))
		require.NoError(t, err)
		return ok
	}
	require.True(t, hasDeadline(777, 1), "exported deadline kept")
	latest := int64(20) + params.ChallengeWindowBlocks + types.ChallengeResolutionTimeoutBlocks
	require.True(t, hasDeadline(latest, 2), "unscheduled challenge gets the latest deadline")
	cursor, err := k.CurationChallengeBackfill.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeBackfillDone, cursor)

	exported, err := k.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, []types.CurationChallengeDeadline{{Deadline: 777, ReviewId: 1}, {Deadline: latest, ReviewId: 2}},
		exported.CurationChallengeDeadlines)

	// A second round trip reproduces the same export.
	f2 := initFixture(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, *exported))
	again, err := f2.keeper.ExportGenesis(f2.ctx)
	require.NoError(t, err)
	require.Equal(t, exported, again)
}
