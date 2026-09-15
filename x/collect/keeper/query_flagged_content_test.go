package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
	commontypes "sparkdream/x/common/types"

	query "github.com/cosmos/cosmos-sdk/types/query"
)

func TestQueryFlaggedContent(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(f *testFixture)
		expLen int
	}{
		{
			name:   "empty - no flagged content",
			setup:  nil,
			expLen: 0,
		},
		{
			name: "returns flagged content in review queue",
			setup: func(f *testFixture) {
				collID := f.createCollection(t, f.owner)
				// Seed flag directly with InReviewQueue=true
				targetType := types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION
				flagKey := keeper.FlagCompositeKey(targetType, collID)
				flag := types.CollectionFlag{
					TargetId:      collID,
					TargetType:    targetType,
					FlagRecords:   []commontypes.FlagRecord{{Flagger: f.member, Reason: commontypes.ModerationReason_MODERATION_REASON_SPAM, Weight: math.NewInt(10)}},
					TotalWeight:   math.NewInt(10),
					FirstFlagAt:   1,
					LastFlagAt:    1,
					InReviewQueue: true,
				}
				err := f.keeper.Flag.Set(f.ctx, flagKey, flag)
				require.NoError(t, err)
				err = f.keeper.FlagReviewQueue.Set(f.ctx, collections.Join(int32(targetType), collID))
				require.NoError(t, err)
			},
			expLen: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			if tc.setup != nil {
				tc.setup(f)
			}
			resp, err := f.queryServer.FlaggedContent(f.ctx, &types.QueryFlaggedContentRequest{})
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Len(t, resp.CollectionFlags, tc.expLen)
		})
	}
}

// seedQueuedFlag puts a collection in the review queue with a live
// CollectionFlag behind it.
func seedQueuedFlag(t *testing.T, f *testFixture, collID uint64) {
	t.Helper()
	targetType := types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION
	flagKey := keeper.FlagCompositeKey(targetType, collID)
	require.NoError(t, f.keeper.Flag.Set(f.ctx, flagKey, types.CollectionFlag{
		TargetId:      collID,
		TargetType:    targetType,
		FlagRecords:   []commontypes.FlagRecord{{Flagger: f.member, Reason: commontypes.ModerationReason_MODERATION_REASON_SPAM, Weight: math.NewInt(10)}},
		TotalWeight:   math.NewInt(10),
		FirstFlagAt:   1,
		LastFlagAt:    1,
		InReviewQueue: true,
	}))
	require.NoError(t, f.keeper.FlagReviewQueue.Set(f.ctx, collections.Join(int32(targetType), collID)))
}

// Total must report the whole queue, not the truncated page, or a client has
// no way to know a second page exists.
func TestQueryFlaggedContent_PaginationTotalAndOffset(t *testing.T) {
	f := initTestFixture(t)
	var ids []uint64
	for i := 0; i < 5; i++ {
		id := f.createCollection(t, f.owner)
		seedQueuedFlag(t, f, id)
		ids = append(ids, id)
	}

	first, err := f.queryServer.FlaggedContent(f.ctx, &types.QueryFlaggedContentRequest{
		Pagination: &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, first.CollectionFlags, 2)
	require.Equal(t, uint64(5), first.Pagination.Total, "total must be the full queue size, not the page size")

	// Page through the rest with offset and make sure every seeded flag shows
	// up exactly once.
	seen := map[uint64]bool{}
	for _, fl := range first.CollectionFlags {
		seen[fl.TargetId] = true
	}
	for offset := uint64(2); offset < 5; offset += 2 {
		page, err := f.queryServer.FlaggedContent(f.ctx, &types.QueryFlaggedContentRequest{
			Pagination: &query.PageRequest{Limit: 2, Offset: offset},
		})
		require.NoError(t, err)
		require.Equal(t, uint64(5), page.Pagination.Total)
		for _, fl := range page.CollectionFlags {
			require.False(t, seen[fl.TargetId], "target %d returned on two pages", fl.TargetId)
			seen[fl.TargetId] = true
		}
	}
	require.Len(t, seen, 5)
	for _, id := range ids {
		require.True(t, seen[id], "collection %d never paged in", id)
	}
}

// An orphaned queue row (index entry whose CollectionFlag was removed) must be
// invisible: it is neither a result nor a step in the offset/total arithmetic.
func TestQueryFlaggedContent_OrphanQueueRowIgnored(t *testing.T) {
	f := initTestFixture(t)
	orphanID := f.createCollection(t, f.owner)
	require.NoError(t, f.keeper.FlagReviewQueue.Set(f.ctx,
		collections.Join(int32(types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION), orphanID)))

	liveID := f.createCollection(t, f.owner)
	seedQueuedFlag(t, f, liveID)

	resp, err := f.queryServer.FlaggedContent(f.ctx, &types.QueryFlaggedContentRequest{})
	require.NoError(t, err)
	require.Len(t, resp.CollectionFlags, 1)
	require.Equal(t, liveID, resp.CollectionFlags[0].TargetId)
	require.Equal(t, uint64(1), resp.Pagination.Total, "orphan row must not inflate total")
}

func TestQueryFlaggedContent_NilRequest(t *testing.T) {
	f := initTestFixture(t)
	_, err := f.queryServer.FlaggedContent(f.ctx, nil)
	require.Error(t, err)
}
