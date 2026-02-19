package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestQueryCollaborators(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(f *testFixture) uint64
		expLen int
	}{
		{
			name: "empty - no collaborators",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			expLen: 0,
		},
		{
			name: "returns collaborator after adding",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				f.addCollaborator(t, collID, f.owner, f.member, types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR)
				return collID
			},
			expLen: 1,
		},
		{
			name: "returns multiple collaborators",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				f.addCollaborator(t, collID, f.owner, f.member, types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR)
				f.addCollaborator(t, collID, f.owner, f.sentinel, types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR)
				return collID
			},
			expLen: 2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			collID := tc.setup(f)
			resp, err := f.queryServer.Collaborators(f.ctx, &types.QueryCollaboratorsRequest{
				CollectionId: collID,
			})
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Len(t, resp.Collaborators, tc.expLen)
		})
	}
}

func TestQueryCollaborators_NilRequest(t *testing.T) {
	f := initTestFixture(t)
	_, err := f.queryServer.Collaborators(f.ctx, nil)
	require.Error(t, err)
}
