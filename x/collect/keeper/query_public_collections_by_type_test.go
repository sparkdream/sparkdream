package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestQueryPublicCollectionsByType(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(f *testFixture)
		reqType uint64
		expLen  int
	}{
		{
			name:    "empty",
			setup:   nil,
			reqType: uint64(types.CollectionType_COLLECTION_TYPE_MIXED),
			expLen:  0,
		},
		{
			name: "returns matching type",
			setup: func(f *testFixture) {
				f.createCollection(t, f.owner, withType(types.CollectionType_COLLECTION_TYPE_MIXED))
			},
			reqType: uint64(types.CollectionType_COLLECTION_TYPE_MIXED),
			expLen:  1,
		},
		{
			name: "excludes non-matching type",
			setup: func(f *testFixture) {
				f.createCollection(t, f.owner, withType(types.CollectionType_COLLECTION_TYPE_MIXED))
			},
			reqType: uint64(types.CollectionType_COLLECTION_TYPE_LINK),
			expLen:  0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			if tc.setup != nil {
				tc.setup(f)
			}
			resp, err := f.queryServer.PublicCollectionsByType(f.ctx, &types.QueryPublicCollectionsByTypeRequest{
				CollectionType: tc.reqType,
			})
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Len(t, resp.Collections, tc.expLen)
		})
	}
}

func TestQueryPublicCollectionsByType_NilRequest(t *testing.T) {
	f := initTestFixture(t)
	_, err := f.queryServer.PublicCollectionsByType(f.ctx, nil)
	require.Error(t, err)
}

// TestQueryPublicCollectionsByType_PinnedFirst mirrors the PublicCollections
// pinned-first guarantee for the type-filtered query: the status index is keyed
// (status, pinned-rank, id), so within the matching type the pinned collection
// must lead even when its ID sorts last. Collections default to MIXED type.
func TestQueryPublicCollectionsByType_PinnedFirst(t *testing.T) {
	f := initTestFixture(t)
	c1 := f.createCollection(t, f.owner) // MIXED
	c2 := f.createCollection(t, f.owner) // MIXED
	c3 := f.createCollection(t, f.owner) // MIXED
	// An off-type collection must not leak into the MIXED result set.
	f.createCollection(t, f.owner, withType(types.CollectionType_COLLECTION_TYPE_LINK))
	f.pin(t, c3)

	resp, err := f.queryServer.PublicCollectionsByType(f.ctx, &types.QueryPublicCollectionsByTypeRequest{
		CollectionType: uint64(types.CollectionType_COLLECTION_TYPE_MIXED),
	})
	require.NoError(t, err)
	require.Len(t, resp.Collections, 3)
	require.Equal(t, c3, resp.Collections[0].Id) // pinned first
	require.Equal(t, c1, resp.Collections[1].Id)
	require.Equal(t, c2, resp.Collections[2].Id)
}
