package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestQueryCurationSummary(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(f *testFixture) uint64
		req    func(collID uint64) *types.QueryCurationSummaryRequest
		expErr bool
		check  func(t *testing.T, resp *types.QueryCurationSummaryResponse, collID uint64)
	}{
		{
			name: "found after rating",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				// Register curator and advance past min_curator_age_blocks
				f.registerCurator(t, f.member, 500)
				f.advanceBlockHeight(14401)
				// Rate the collection
				_, err := f.msgServer.RateCollection(f.ctx, &types.MsgRateCollection{
					Creator:      f.member,
					CollectionId: collID,
					Verdict:      types.CurationVerdict_CURATION_VERDICT_UP,
					Tags:         []string{"quality"},
					Comment:      "good collection",
				})
				require.NoError(t, err)
				return collID
			},
			req: func(collID uint64) *types.QueryCurationSummaryRequest {
				return &types.QueryCurationSummaryRequest{CollectionId: collID}
			},
			expErr: false,
			check: func(t *testing.T, resp *types.QueryCurationSummaryResponse, collID uint64) {
				require.Equal(t, collID, resp.Summary.CollectionId)
				require.Equal(t, uint32(1), resp.Summary.UpCount)
			},
		},
		{
			name:  "not found",
			setup: nil,
			req: func(_ uint64) *types.QueryCurationSummaryRequest {
				return &types.QueryCurationSummaryRequest{CollectionId: 999}
			},
			expErr: true,
		},
		{
			name:  "nil request",
			setup: nil,
			req: func(_ uint64) *types.QueryCurationSummaryRequest {
				return nil
			},
			expErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			var collID uint64
			if tc.setup != nil {
				collID = tc.setup(f)
			}
			resp, err := f.queryServer.CurationSummary(f.ctx, tc.req(collID))
			if tc.expErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp)
			if tc.check != nil {
				tc.check(t, resp, collID)
			}
		})
	}
}
