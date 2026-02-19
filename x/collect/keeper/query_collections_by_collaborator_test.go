package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestQueryCollectionsByCollaborator(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(f *testFixture)
		req    func(f *testFixture) *types.QueryCollectionsByCollaboratorRequest
		expLen int
	}{
		{
			name:  "empty",
			setup: nil,
			req: func(f *testFixture) *types.QueryCollectionsByCollaboratorRequest {
				return &types.QueryCollectionsByCollaboratorRequest{Address: f.member}
			},
			expLen: 0,
		},
		{
			name: "returns collection after adding collaborator",
			setup: func(f *testFixture) {
				collID := f.createCollection(t, f.owner)
				f.addCollaborator(t, collID, f.owner, f.member, types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR)
			},
			req: func(f *testFixture) *types.QueryCollectionsByCollaboratorRequest {
				return &types.QueryCollectionsByCollaboratorRequest{Address: f.member}
			},
			expLen: 1,
		},
		{
			name:  "nil request returns error",
			setup: nil,
			req: func(_ *testFixture) *types.QueryCollectionsByCollaboratorRequest {
				return nil
			},
			expLen: -1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			if tc.setup != nil {
				tc.setup(f)
			}
			resp, err := f.queryServer.CollectionsByCollaborator(f.ctx, tc.req(f))
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
