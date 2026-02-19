package keeper_test

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestAddCollaborator(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture) uint64
		msg            func(f *testFixture, collID uint64) *types.MsgAddCollaborator
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture, collID uint64)
	}{
		{
			name: "success: owner adds EDITOR collaborator",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			msg: func(f *testFixture, collID uint64) *types.MsgAddCollaborator {
				return &types.MsgAddCollaborator{
					Creator:      f.owner,
					CollectionId: collID,
					Address:      f.member,
					Role:         types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR,
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, collID uint64) {
				coll, err := f.keeper.Collection.Get(f.ctx, collID)
				require.NoError(t, err)
				require.Equal(t, uint32(1), coll.CollaboratorCount)
			},
		},
		{
			name: "error: target is not a member",
			setup: func(f *testFixture) uint64 {
				// Override isMemberFn to return false for nonMember
				f.repKeeper.isMemberFn = func(_ context.Context, addr sdk.AccAddress) bool {
					return addr.Equals(f.ownerAddr) || addr.Equals(f.memberAddr) || addr.Equals(f.sentinelAddr)
				}
				return f.createCollection(t, f.owner)
			},
			msg: func(f *testFixture, collID uint64) *types.MsgAddCollaborator {
				return &types.MsgAddCollaborator{
					Creator:      f.owner,
					CollectionId: collID,
					Address:      f.nonMember,
					Role:         types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR,
				}
			},
			expErr:         true,
			expErrContains: "not an active x/rep member",
		},
		{
			name: "error: already collaborator",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				f.addCollaborator(t, collID, f.owner, f.member, types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgAddCollaborator {
				return &types.MsgAddCollaborator{
					Creator:      f.owner,
					CollectionId: collID,
					Address:      f.member,
					Role:         types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR,
				}
			},
			expErr:         true,
			expErrContains: "already a collaborator",
		},
		{
			name: "error: collection is immutable",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				coll, _ := f.keeper.Collection.Get(f.ctx, collID)
				coll.Immutable = true
				f.keeper.Collection.Set(f.ctx, collID, coll)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgAddCollaborator {
				return &types.MsgAddCollaborator{
					Creator:      f.owner,
					CollectionId: collID,
					Address:      f.member,
					Role:         types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR,
				}
			},
			expErr:         true,
			expErrContains: "immutable",
		},
		{
			name: "error: max collaborators reached",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				// Set collaborator_count to max
				coll, _ := f.keeper.Collection.Get(f.ctx, collID)
				coll.CollaboratorCount = 20
				f.keeper.Collection.Set(f.ctx, collID, coll)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgAddCollaborator {
				return &types.MsgAddCollaborator{
					Creator:      f.owner,
					CollectionId: collID,
					Address:      f.member,
					Role:         types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR,
				}
			},
			expErr:         true,
			expErrContains: "max collaborators",
		},
		{
			name: "error: only owner can grant ADMIN",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				// Add member as ADMIN so they can call AddCollaborator
				f.addCollaborator(t, collID, f.owner, f.member, types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgAddCollaborator {
				return &types.MsgAddCollaborator{
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
			resp, err := f.msgServer.AddCollaborator(f.ctx, msg)
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
