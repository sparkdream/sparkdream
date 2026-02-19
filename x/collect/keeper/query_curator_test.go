package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestQueryCurator(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(f *testFixture)
		req    func(f *testFixture) *types.QueryCuratorRequest
		expErr bool
		check  func(t *testing.T, resp *types.QueryCuratorResponse, f *testFixture)
	}{
		{
			name: "found",
			setup: func(f *testFixture) {
				f.registerCurator(t, f.member, 500)
			},
			req: func(f *testFixture) *types.QueryCuratorRequest {
				return &types.QueryCuratorRequest{Address: f.member}
			},
			expErr: false,
			check: func(t *testing.T, resp *types.QueryCuratorResponse, f *testFixture) {
				require.Equal(t, f.member, resp.Curator.Address)
				require.True(t, resp.Curator.Active)
			},
		},
		{
			name:  "not found",
			setup: nil,
			req: func(f *testFixture) *types.QueryCuratorRequest {
				return &types.QueryCuratorRequest{Address: f.owner}
			},
			expErr: true,
		},
		{
			name:  "nil request",
			setup: nil,
			req: func(_ *testFixture) *types.QueryCuratorRequest {
				return nil
			},
			expErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			if tc.setup != nil {
				tc.setup(f)
			}
			resp, err := f.queryServer.Curator(f.ctx, tc.req(f))
			if tc.expErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp)
			if tc.check != nil {
				tc.check(t, resp, f)
			}
		})
	}
}
