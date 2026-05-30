package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
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
		{
			name: "success: non-member removal on ACTIVE refunds full stake",
			setup: func(f *testFixture) uint64 {
				f.repKeeper.isMemberFn = func(_ context.Context, addr sdk.AccAddress) bool {
					return addr.Equals(f.ownerAddr) || addr.Equals(f.memberAddr) || addr.Equals(f.sentinelAddr)
				}
				collID := f.createCollection(t, f.owner)
				_, err := f.msgServer.AddCollaborator(f.ctx, &types.MsgAddCollaborator{
					Creator:      f.owner,
					CollectionId: collID,
					Address:      f.nonMember,
					Role:         types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR,
				})
				require.NoError(t, err)
				// Track unlock + burn calls
				f.repKeeper.unlockDREAMFn = func(_ context.Context, addr sdk.AccAddress, amount math.Int) error {
					f.repKeeper.unlockCalls = append(f.repKeeper.unlockCalls, dreamCall{addr: addr, amount: amount})
					return nil
				}
				f.repKeeper.burnDREAMFn = func(_ context.Context, addr sdk.AccAddress, amount math.Int) error {
					f.repKeeper.burnCalls = append(f.repKeeper.burnCalls, dreamCall{addr: addr, amount: amount})
					return nil
				}
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgRemoveCollaborator {
				return &types.MsgRemoveCollaborator{
					Creator:      f.owner,
					CollectionId: collID,
					Address:      f.nonMember,
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, collID uint64) {
				params, _ := f.keeper.Params.Get(f.ctx)
				require.Len(t, f.repKeeper.unlockCalls, 1)
				require.True(t, f.repKeeper.unlockCalls[0].addr.Equals(f.ownerAddr))
				require.True(t, f.repKeeper.unlockCalls[0].amount.Equal(params.NonMemberCollabDreamStake))
				require.Empty(t, f.repKeeper.burnCalls, "ACTIVE collection must not burn")

				coll, _ := f.keeper.Collection.Get(f.ctx, collID)
				require.Equal(t, uint32(0), coll.CollaboratorCount)
				require.Equal(t, uint32(0), coll.NonMemberCollaboratorCount)
			},
		},
		{
			name: "success: non-member removal on HIDDEN burns fraction, refunds rest",
			setup: func(f *testFixture) uint64 {
				f.repKeeper.isMemberFn = func(_ context.Context, addr sdk.AccAddress) bool {
					return addr.Equals(f.ownerAddr) || addr.Equals(f.memberAddr) || addr.Equals(f.sentinelAddr)
				}
				collID := f.createCollection(t, f.owner)
				_, err := f.msgServer.AddCollaborator(f.ctx, &types.MsgAddCollaborator{
					Creator:      f.owner,
					CollectionId: collID,
					Address:      f.nonMember,
					Role:         types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR,
				})
				require.NoError(t, err)
				// Flip collection to HIDDEN
				coll, _ := f.keeper.Collection.Get(f.ctx, collID)
				coll.Status = types.CollectionStatus_COLLECTION_STATUS_HIDDEN
				f.keeper.Collection.Set(f.ctx, collID, coll)

				f.repKeeper.unlockDREAMFn = func(_ context.Context, addr sdk.AccAddress, amount math.Int) error {
					f.repKeeper.unlockCalls = append(f.repKeeper.unlockCalls, dreamCall{addr: addr, amount: amount})
					return nil
				}
				f.repKeeper.burnDREAMFn = func(_ context.Context, addr sdk.AccAddress, amount math.Int) error {
					f.repKeeper.burnCalls = append(f.repKeeper.burnCalls, dreamCall{addr: addr, amount: amount})
					return nil
				}
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgRemoveCollaborator {
				return &types.MsgRemoveCollaborator{
					Creator:      f.owner,
					CollectionId: collID,
					Address:      f.nonMember,
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, collID uint64) {
				params, _ := f.keeper.Params.Get(f.ctx)
				expectedBurn := params.NonMemberCollabBurnFraction.MulInt(params.NonMemberCollabDreamStake).TruncateInt()

				require.Len(t, f.repKeeper.unlockCalls, 1)
				require.True(t, f.repKeeper.unlockCalls[0].amount.Equal(params.NonMemberCollabDreamStake))

				require.Len(t, f.repKeeper.burnCalls, 1)
				require.True(t, f.repKeeper.burnCalls[0].addr.Equals(f.ownerAddr))
				require.True(t, f.repKeeper.burnCalls[0].amount.Equal(expectedBurn),
					"expected burn %s, got %s", expectedBurn, f.repKeeper.burnCalls[0].amount)
			},
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
