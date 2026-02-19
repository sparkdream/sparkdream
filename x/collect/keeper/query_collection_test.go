package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestQueryCollection(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(f *testFixture) uint64
		req    func(id uint64) *types.QueryCollectionRequest
		expErr bool
		check  func(t *testing.T, resp *types.QueryCollectionResponse, id uint64)
	}{
		{
			name: "found",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			req:    func(id uint64) *types.QueryCollectionRequest { return &types.QueryCollectionRequest{Id: id} },
			expErr: false,
			check: func(t *testing.T, resp *types.QueryCollectionResponse, id uint64) {
				require.Equal(t, id, resp.Collection.Id)
				require.Equal(t, "test-collection", resp.Collection.Name)
			},
		},
		{
			name:   "not found",
			setup:  nil,
			req:    func(_ uint64) *types.QueryCollectionRequest { return &types.QueryCollectionRequest{Id: 999} },
			expErr: true,
		},
		{
			name:   "nil request",
			setup:  nil,
			req:    func(_ uint64) *types.QueryCollectionRequest { return nil },
			expErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			var id uint64
			if tc.setup != nil {
				id = tc.setup(f)
			}
			resp, err := f.queryServer.Collection(f.ctx, tc.req(id))
			if tc.expErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp)
			if tc.check != nil {
				tc.check(t, resp, id)
			}
		})
	}
}
