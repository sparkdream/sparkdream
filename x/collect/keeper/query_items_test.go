package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestQueryItems(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(f *testFixture) uint64
		expLen int
	}{
		{
			name: "empty collection",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			expLen: 0,
		},
		{
			name: "returns items in collection",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				f.addItem(t, collID, f.owner)
				f.addItem(t, collID, f.owner)
				return collID
			},
			expLen: 2,
		},
		{
			name: "items from other collections not included",
			setup: func(f *testFixture) uint64 {
				collID1 := f.createCollection(t, f.owner)
				collID2 := f.createCollection(t, f.owner)
				f.addItem(t, collID1, f.owner)
				f.addItem(t, collID2, f.owner)
				return collID1
			},
			expLen: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			collID := tc.setup(f)
			resp, err := f.queryServer.Items(f.ctx, &types.QueryItemsRequest{CollectionId: collID})
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Len(t, resp.Items, tc.expLen)
		})
	}
}

func TestQueryItems_NilRequest(t *testing.T) {
	f := initTestFixture(t)
	_, err := f.queryServer.Items(f.ctx, nil)
	require.Error(t, err)
}
