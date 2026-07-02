package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestQueryCollectionsByOwner(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(f *testFixture)
		req    func(f *testFixture) *types.QueryCollectionsByOwnerRequest
		expLen int
	}{
		{
			name:  "empty",
			setup: nil,
			req: func(f *testFixture) *types.QueryCollectionsByOwnerRequest {
				return &types.QueryCollectionsByOwnerRequest{Owner: f.owner}
			},
			expLen: 0,
		},
		{
			name: "returns owned collections",
			setup: func(f *testFixture) {
				f.createCollection(t, f.owner)
				f.createCollection(t, f.owner)
			},
			req: func(f *testFixture) *types.QueryCollectionsByOwnerRequest {
				return &types.QueryCollectionsByOwnerRequest{Owner: f.owner}
			},
			expLen: 2,
		},
		{
			name: "different owner returns empty",
			setup: func(f *testFixture) {
				f.createCollection(t, f.owner)
			},
			req: func(f *testFixture) *types.QueryCollectionsByOwnerRequest {
				return &types.QueryCollectionsByOwnerRequest{Owner: f.member}
			},
			expLen: 0,
		},
		{
			name:  "nil request returns error",
			setup: nil,
			req: func(_ *testFixture) *types.QueryCollectionsByOwnerRequest {
				return nil
			},
			expLen: -1, // sentinel for error
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			if tc.setup != nil {
				tc.setup(f)
			}
			resp, err := f.queryServer.CollectionsByOwner(f.ctx, tc.req(f))
			if tc.expLen == -1 {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Len(t, resp.Collections, tc.expLen)
		})
	}
}

func TestQueryCollectionsByOwner_PinnedFirst(t *testing.T) {
	f := initTestFixture(t)
	// Three owned collections; pin the highest-ID one. It must come back first
	// even though it sorts last by ID.
	c1 := f.createCollection(t, f.owner)
	c2 := f.createCollection(t, f.owner)
	c3 := f.createCollection(t, f.owner)
	f.pin(t, c3)

	resp, err := f.queryServer.CollectionsByOwner(f.ctx, &types.QueryCollectionsByOwnerRequest{Owner: f.owner})
	require.NoError(t, err)
	require.Len(t, resp.Collections, 3)
	require.Equal(t, c3, resp.Collections[0].Id)
	require.Equal(t, c1, resp.Collections[1].Id)
	require.Equal(t, c2, resp.Collections[2].Id)
}
