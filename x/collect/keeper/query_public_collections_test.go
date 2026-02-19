package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestQueryPublicCollections(t *testing.T) {
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
			name: "returns public active collection",
			setup: func(f *testFixture) {
				// createCollection creates a PUBLIC ACTIVE collection by default (owner is member)
				f.createCollection(t, f.owner)
			},
			expLen: 1,
		},
		{
			name: "excludes pending collections",
			setup: func(f *testFixture) {
				// PENDING (non-member) collections should NOT be returned
				f.createPendingCollection(t)
			},
			expLen: 0,
		},
		{
			name: "multiple public active collections",
			setup: func(f *testFixture) {
				f.createCollection(t, f.owner)
				f.createCollection(t, f.owner)
			},
			expLen: 2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			if tc.setup != nil {
				tc.setup(f)
			}
			resp, err := f.queryServer.PublicCollections(f.ctx, &types.QueryPublicCollectionsRequest{})
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Len(t, resp.Collections, tc.expLen)
		})
	}
}

func TestQueryPublicCollections_NilRequest(t *testing.T) {
	f := initTestFixture(t)
	_, err := f.queryServer.PublicCollections(f.ctx, nil)
	require.Error(t, err)
}
