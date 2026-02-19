package keeper_test

import (
	"testing"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"

	"github.com/stretchr/testify/require"
)

func TestUpdateCollaboratorRole(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture) uint64
		msg            func(f *testFixture, collID uint64) *types.MsgUpdateCollaboratorRole
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture, collID uint64)
	}{
		{
			name: "success: owner changes EDITOR to ADMIN",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				f.addCollaborator(t, collID, f.owner, f.member, types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgUpdateCollaboratorRole {
				return &types.MsgUpdateCollaboratorRole{
					Creator:      f.owner,
					CollectionId: collID,
					Address:      f.member,
					Role:         types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN,
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, collID uint64) {
				compositeKey := keeper.CollaboratorCompositeKey(collID, f.member)
				collab, err := f.keeper.Collaborator.Get(f.ctx, compositeKey)
				require.NoError(t, err)
				require.Equal(t, types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN, collab.Role)
			},
		},
		{
			name: "error: ADMIN cannot change another ADMIN (non-owner)",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				f.addCollaborator(t, collID, f.owner, f.member, types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN)
				f.addCollaborator(t, collID, f.owner, f.sentinel, types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgUpdateCollaboratorRole {
				return &types.MsgUpdateCollaboratorRole{
					Creator:      f.member,
					CollectionId: collID,
					Address:      f.sentinel,
					Role:         types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR,
				}
			},
			expErr:         true,
			expErrContains: "only owner can grant/revoke ADMIN",
		},
		{
			name: "error: non-owner cannot grant ADMIN",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				f.addCollaborator(t, collID, f.owner, f.member, types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN)
				f.addCollaborator(t, collID, f.owner, f.sentinel, types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgUpdateCollaboratorRole {
				return &types.MsgUpdateCollaboratorRole{
					Creator:      f.member,
					CollectionId: collID,
					Address:      f.sentinel,
					Role:         types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN,
				}
			},
			expErr:         true,
			expErrContains: "only owner can grant/revoke ADMIN",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			collID := tc.setup(f)
			msg := tc.msg(f, collID)
			resp, err := f.msgServer.UpdateCollaboratorRole(f.ctx, msg)
			if tc.expErr {
				require.Error(t, err)
				if tc.expErrContains != "" {
					require.Contains(t, err.Error(), tc.expErrContains)
				}
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp)
			if tc.check != nil {
				tc.check(t, f, collID)
			}
		})
	}
}
