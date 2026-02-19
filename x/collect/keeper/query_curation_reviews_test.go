package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestQueryCurationReviews(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(f *testFixture) uint64
		expLen int
	}{
		{
			name: "empty - no reviews",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			expLen: 0,
		},
		{
			name: "returns review after rating",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				f.registerCurator(t, f.member, 500)
				f.advanceBlockHeight(14401)
				_, err := f.msgServer.RateCollection(f.ctx, &types.MsgRateCollection{
					Creator:      f.member,
					CollectionId: collID,
					Verdict:      types.CurationVerdict_CURATION_VERDICT_UP,
					Tags:         []string{"quality"},
				})
				require.NoError(t, err)
				return collID
			},
			expLen: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			collID := tc.setup(f)
			resp, err := f.queryServer.CurationReviews(f.ctx, &types.QueryCurationReviewsRequest{
				CollectionId: collID,
			})
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Len(t, resp.Reviews, tc.expLen)
		})
	}
}

func TestQueryCurationReviews_NilRequest(t *testing.T) {
	f := initTestFixture(t)
	_, err := f.queryServer.CurationReviews(f.ctx, nil)
	require.Error(t, err)
}
