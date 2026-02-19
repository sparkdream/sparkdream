package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestQueryPendingCollections(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(f *testFixture)
		expLen int
	}{
		{
			name:   "empty",
			setup:  nil,
			expLen: 0,
		},
		{
			name: "returns pending collection with seeking endorsement",
			setup: func(f *testFixture) {
				collID := f.createPendingCollection(t)
				// PendingCollections query filters by SeekingEndorsement=true
				_, err := f.msgServer.SetSeekingEndorsement(f.ctx, &types.MsgSetSeekingEndorsement{
					Creator:      f.nonMember,
					CollectionId: collID,
					Seeking:      true,
				})
				require.NoError(t, err)
			},
			expLen: 1,
		},
		{
			name: "excludes pending without seeking endorsement",
			setup: func(f *testFixture) {
				// createPendingCollection creates a PENDING collection but SeekingEndorsement defaults to false
				f.createPendingCollection(t)
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
			resp, err := f.queryServer.PendingCollections(f.ctx, &types.QueryPendingCollectionsRequest{})
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Len(t, resp.Collections, tc.expLen)
		})
	}
}

func TestQueryPendingCollections_NilRequest(t *testing.T) {
	f := initTestFixture(t)
	_, err := f.queryServer.PendingCollections(f.ctx, nil)
	require.Error(t, err)
}
