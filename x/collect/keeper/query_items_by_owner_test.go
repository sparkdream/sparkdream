package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestQueryItemsByOwner(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(f *testFixture)
		req    func(f *testFixture) *types.QueryItemsByOwnerRequest
		expLen int
	}{
		{
			name:  "empty",
			setup: nil,
			req: func(f *testFixture) *types.QueryItemsByOwnerRequest {
				return &types.QueryItemsByOwnerRequest{Owner: f.owner}
			},
			expLen: 0,
		},
		{
			name: "returns items owned by collection owner",
			setup: func(f *testFixture) {
				collID := f.createCollection(t, f.owner)
				f.addItem(t, collID, f.owner)
				f.addItem(t, collID, f.owner)
			},
			req: func(f *testFixture) *types.QueryItemsByOwnerRequest {
				return &types.QueryItemsByOwnerRequest{Owner: f.owner}
			},
			expLen: 2,
		},
		{
			name: "different owner returns empty",
			setup: func(f *testFixture) {
				collID := f.createCollection(t, f.owner)
				f.addItem(t, collID, f.owner)
			},
			req: func(f *testFixture) *types.QueryItemsByOwnerRequest {
				return &types.QueryItemsByOwnerRequest{Owner: f.member}
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
			resp, err := f.queryServer.ItemsByOwner(f.ctx, tc.req(f))
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Len(t, resp.Items, tc.expLen)
		})
	}
}

func TestQueryItemsByOwner_NilRequest(t *testing.T) {
	f := initTestFixture(t)
	_, err := f.queryServer.ItemsByOwner(f.ctx, nil)
	require.Error(t, err)
}
