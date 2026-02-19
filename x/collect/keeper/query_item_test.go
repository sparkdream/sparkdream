package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestQueryItem(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(f *testFixture) uint64
		req    func(id uint64) *types.QueryItemRequest
		expErr bool
		check  func(t *testing.T, resp *types.QueryItemResponse)
	}{
		{
			name: "found",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				return f.addItem(t, collID, f.owner)
			},
			req:    func(id uint64) *types.QueryItemRequest { return &types.QueryItemRequest{Id: id} },
			expErr: false,
			check: func(t *testing.T, resp *types.QueryItemResponse) {
				require.Equal(t, "test-item", resp.Item.Title)
			},
		},
		{
			name:   "not found",
			setup:  nil,
			req:    func(_ uint64) *types.QueryItemRequest { return &types.QueryItemRequest{Id: 999} },
			expErr: true,
		},
		{
			name:   "nil request",
			setup:  nil,
			req:    func(_ uint64) *types.QueryItemRequest { return nil },
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
			resp, err := f.queryServer.Item(f.ctx, tc.req(id))
			if tc.expErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp)
			if tc.check != nil {
				tc.check(t, resp)
			}
		})
	}
}
