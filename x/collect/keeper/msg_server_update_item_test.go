package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestUpdateItem(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture) (collID uint64, itemID uint64)
		msg            func(f *testFixture, itemID uint64) *types.MsgUpdateItem
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture, itemID uint64)
	}{
		{
			name: "owner updates item fields",
			setup: func(f *testFixture) (uint64, uint64) {
				collID := f.createCollection(t, f.owner)
				itemID := f.addItem(t, collID, f.owner)
				return collID, itemID
			},
			msg: func(f *testFixture, itemID uint64) *types.MsgUpdateItem {
				return &types.MsgUpdateItem{
					Creator:     f.owner,
					Id:          itemID,
					Title:       "updated-title",
					Description: "updated-desc",
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, itemID uint64) {
				item, err := f.keeper.Item.Get(f.ctx, itemID)
				require.NoError(t, err)
				require.Equal(t, "updated-title", item.Title)
				require.Equal(t, "updated-desc", item.Description)
			},
		},
		{
			name: "editor collaborator updates item",
			setup: func(f *testFixture) (uint64, uint64) {
				collID := f.createCollection(t, f.owner)
				f.addCollaborator(t, collID, f.owner, f.member, types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR)
				itemID := f.addItem(t, collID, f.owner)
				return collID, itemID
			},
			msg: func(f *testFixture, itemID uint64) *types.MsgUpdateItem {
				return &types.MsgUpdateItem{
					Creator:     f.member,
					Id:          itemID,
					Title:       "editor-update",
					Description: "by editor",
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, itemID uint64) {
				item, err := f.keeper.Item.Get(f.ctx, itemID)
				require.NoError(t, err)
				require.Equal(t, "editor-update", item.Title)
			},
		},
		{
			name: "error: item not found",
			setup: func(f *testFixture) (uint64, uint64) {
				return 0, 999999
			},
			msg: func(f *testFixture, itemID uint64) *types.MsgUpdateItem {
				return &types.MsgUpdateItem{
					Creator: f.owner,
					Id:      itemID,
					Title:   "no-item",
				}
			},
			expErr:         true,
			expErrContains: "item not found",
		},
		{
			name: "error: not authorized (non-collaborator member)",
			setup: func(f *testFixture) (uint64, uint64) {
				collID := f.createCollection(t, f.owner)
				// member is NOT a collaborator, so no write access
				itemID := f.addItem(t, collID, f.owner)
				return collID, itemID
			},
			msg: func(f *testFixture, itemID uint64) *types.MsgUpdateItem {
				return &types.MsgUpdateItem{
					Creator: f.member,
					Id:      itemID,
					Title:   "unauthorized-edit",
				}
			},
			expErr:         true,
			expErrContains: "unauthorized",
		},
		{
			name: "error: immutable collection",
			setup: func(f *testFixture) (uint64, uint64) {
				collID := f.createCollection(t, f.owner)
				itemID := f.addItem(t, collID, f.owner)
				// Set immutable
				coll, _ := f.keeper.Collection.Get(f.ctx, collID)
				coll.Immutable = true
				f.keeper.Collection.Set(f.ctx, collID, coll)
				return collID, itemID
			},
			msg: func(f *testFixture, itemID uint64) *types.MsgUpdateItem {
				return &types.MsgUpdateItem{
					Creator: f.owner,
					Id:      itemID,
					Title:   "immutable-edit",
				}
			},
			expErr:         true,
			expErrContains: "immutable",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)

			var itemID uint64
			if tc.setup != nil {
				_, itemID = tc.setup(f)
			}

			msg := tc.msg(f, itemID)
			_, err := f.msgServer.UpdateItem(f.ctx, msg)
			if tc.expErr {
				require.Error(t, err)
				if tc.expErrContains != "" {
					require.Contains(t, err.Error(), tc.expErrContains)
				}
				return
			}
			require.NoError(t, err)
			if tc.check != nil {
				tc.check(t, f, itemID)
			}
		})
	}
}
