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
