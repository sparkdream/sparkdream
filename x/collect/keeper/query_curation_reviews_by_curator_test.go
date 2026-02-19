package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestQueryCurationReviewsByCurator(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(f *testFixture)
		req    func(f *testFixture) *types.QueryCurationReviewsByCuratorRequest
		expLen int
	}{
		{
			name:  "empty - no reviews",
			setup: nil,
			req: func(f *testFixture) *types.QueryCurationReviewsByCuratorRequest {
				return &types.QueryCurationReviewsByCuratorRequest{Curator: f.member}
			},
			expLen: 0,
		},
		{
			name: "returns reviews by curator",
			setup: func(f *testFixture) {
				collID := f.createCollection(t, f.owner)
				f.registerCurator(t, f.member, 500)
				f.advanceBlockHeight(14401)
				_, err := f.msgServer.RateCollection(f.ctx, &types.MsgRateCollection{
					Creator:      f.member,
					CollectionId: collID,
					Verdict:      types.CurationVerdict_CURATION_VERDICT_DOWN,
					Tags:         []string{"low-quality"},
				})
				require.NoError(t, err)
			},
			req: func(f *testFixture) *types.QueryCurationReviewsByCuratorRequest {
				return &types.QueryCurationReviewsByCuratorRequest{Curator: f.member}
			},
			expLen: 1,
		},
		{
			name: "different curator returns empty",
			setup: func(f *testFixture) {
				collID := f.createCollection(t, f.owner)
				f.registerCurator(t, f.member, 500)
				f.advanceBlockHeight(14401)
				_, err := f.msgServer.RateCollection(f.ctx, &types.MsgRateCollection{
					Creator:      f.member,
					CollectionId: collID,
					Verdict:      types.CurationVerdict_CURATION_VERDICT_UP,
				})
				require.NoError(t, err)
			},
			req: func(f *testFixture) *types.QueryCurationReviewsByCuratorRequest {
				return &types.QueryCurationReviewsByCuratorRequest{Curator: f.owner}
			},
			expLen: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			if tc.setup != nil {
				tc.setup(f)
			}
			resp, err := f.queryServer.CurationReviewsByCurator(f.ctx, tc.req(f))
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Len(t, resp.Reviews, tc.expLen)
		})
	}
}

func TestQueryCurationReviewsByCurator_NilRequest(t *testing.T) {
	f := initTestFixture(t)
	_, err := f.queryServer.CurationReviewsByCurator(f.ctx, nil)
	require.Error(t, err)
}
