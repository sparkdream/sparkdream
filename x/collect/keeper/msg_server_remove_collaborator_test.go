package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestRemoveCollaborator(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture) uint64
		msg            func(f *testFixture, collID uint64) *types.MsgRemoveCollaborator
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture, collID uint64)
	}{
		{
			name: "success: owner removes collaborator",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				f.addCollaborator(t, collID, f.owner, f.member, types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgRemoveCollaborator {
				return &types.MsgRemoveCollaborator{
					Creator:      f.owner,
					CollectionId: collID,
					Address:      f.member,
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, collID uint64) {
				coll, err := f.keeper.Collection.Get(f.ctx, collID)
				require.NoError(t, err)
				require.Equal(t, uint32(0), coll.CollaboratorCount)
			},
		},
		{
			name: "success: self-removal even if immutable",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				f.addCollaborator(t, collID, f.owner, f.member, types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR)
				// Make collection immutable
				coll, _ := f.keeper.Collection.Get(f.ctx, collID)
				coll.Immutable = true
				f.keeper.Collection.Set(f.ctx, collID, coll)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgRemoveCollaborator {
				return &types.MsgRemoveCollaborator{
					Creator:      f.member,
					CollectionId: collID,
					Address:      f.member,
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, collID uint64) {
				coll, err := f.keeper.Collection.Get(f.ctx, collID)
				require.NoError(t, err)
				require.Equal(t, uint32(0), coll.CollaboratorCount)
			},
		},
		{
			name: "error: collaborator not found",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			msg: func(f *testFixture, collID uint64) *types.MsgRemoveCollaborator {
				return &types.MsgRemoveCollaborator{
					Creator:      f.owner,
					CollectionId: collID,
					Address:      f.member,
				}
			},
			expErr:         true,
			expErrContains: "not a collaborator",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			collID := tc.setup(f)
			msg := tc.msg(f, collID)
			resp, err := f.msgServer.RemoveCollaborator(f.ctx, msg)
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
