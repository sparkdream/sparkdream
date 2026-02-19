package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
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
					FlagRecords:   []types.FlagRecord{{Flagger: f.member, Reason: types.ModerationReason_MODERATION_REASON_SPAM, Weight: math.NewInt(10)}},
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

func TestQueryFlaggedContent_NilRequest(t *testing.T) {
	f := initTestFixture(t)
	_, err := f.queryServer.FlaggedContent(f.ctx, nil)
	require.Error(t, err)
}
