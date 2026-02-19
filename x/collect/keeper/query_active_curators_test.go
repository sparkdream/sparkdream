package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestQueryActiveCurators(t *testing.T) {
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
			name: "returns active curator",
			setup: func(f *testFixture) {
				f.registerCurator(t, f.member, 500)
			},
			expLen: 1,
		},
		{
			name: "multiple active curators",
			setup: func(f *testFixture) {
				f.registerCurator(t, f.member, 500)
				f.registerCurator(t, f.owner, 600)
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
			resp, err := f.queryServer.ActiveCurators(f.ctx, &types.QueryActiveCuratorsRequest{})
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Len(t, resp.Curators, tc.expLen)
		})
	}
}

func TestQueryActiveCurators_NilRequest(t *testing.T) {
	f := initTestFixture(t)
	_, err := f.queryServer.ActiveCurators(f.ctx, nil)
	require.Error(t, err)
}
