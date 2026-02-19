package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestRemoveItems(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture) (collID uint64, itemIDs []uint64)
		msg            func(f *testFixture, itemIDs []uint64) *types.MsgRemoveItems
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture, collID uint64, itemIDs []uint64)
	}{
		{
			name: "batch remove multiple items",
			setup: func(f *testFixture) (uint64, []uint64) {
				collID := f.createCollection(t, f.owner)
				id1 := f.addItem(t, collID, f.owner)
				id2 := f.addItem(t, collID, f.owner)
				id3 := f.addItem(t, collID, f.owner)
				return collID, []uint64{id1, id2, id3}
			},
			msg: func(f *testFixture, itemIDs []uint64) *types.MsgRemoveItems {
				return &types.MsgRemoveItems{
					Creator: f.owner,
					Ids:     []uint64{itemIDs[0], itemIDs[2]}, // remove first and third
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, collID uint64, itemIDs []uint64) {
				// Removed items should not exist
				_, err := f.keeper.Item.Get(f.ctx, itemIDs[0])
				require.Error(t, err)
				_, err = f.keeper.Item.Get(f.ctx, itemIDs[2])
				require.Error(t, err)

				// Remaining item should exist
				_, err = f.keeper.Item.Get(f.ctx, itemIDs[1])
				require.NoError(t, err)

				// Collection item_count decremented
				coll, err := f.keeper.Collection.Get(f.ctx, collID)
				require.NoError(t, err)
				require.Equal(t, uint64(1), coll.ItemCount)
			},
		},
		{
			name: "error: duplicate item IDs",
			setup: func(f *testFixture) (uint64, []uint64) {
				collID := f.createCollection(t, f.owner)
				id1 := f.addItem(t, collID, f.owner)
				return collID, []uint64{id1}
			},
			msg: func(f *testFixture, itemIDs []uint64) *types.MsgRemoveItems {
				return &types.MsgRemoveItems{
					Creator: f.owner,
					Ids:     []uint64{itemIDs[0], itemIDs[0]}, // duplicate
				}
			},
			expErr:         true,
			expErrContains: "duplicate item IDs",
		},
		{
			name: "error: items from different collections",
			setup: func(f *testFixture) (uint64, []uint64) {
				collID1 := f.createCollection(t, f.owner)
				collID2 := f.createCollection(t, f.owner)
				id1 := f.addItem(t, collID1, f.owner)
				id2 := f.addItem(t, collID2, f.owner)
				return collID1, []uint64{id1, id2}
			},
			msg: func(f *testFixture, itemIDs []uint64) *types.MsgRemoveItems {
				return &types.MsgRemoveItems{
					Creator: f.owner,
					Ids:     itemIDs,
				}
			},
			expErr:         true,
			expErrContains: "batch items span multiple collections",
		},
		{
			name: "error: empty batch",
			setup: func(f *testFixture) (uint64, []uint64) {
				return 0, nil
			},
			msg: func(f *testFixture, itemIDs []uint64) *types.MsgRemoveItems {
				return &types.MsgRemoveItems{
					Creator: f.owner,
					Ids:     []uint64{},
				}
			},
			expErr:         true,
			expErrContains: "empty batch",
		},
		{
			name: "error: item not found in batch",
			setup: func(f *testFixture) (uint64, []uint64) {
				collID := f.createCollection(t, f.owner)
				id1 := f.addItem(t, collID, f.owner)
				return collID, []uint64{id1}
			},
			msg: func(f *testFixture, itemIDs []uint64) *types.MsgRemoveItems {
				return &types.MsgRemoveItems{
					Creator: f.owner,
					Ids:     []uint64{itemIDs[0], 999999},
				}
			},
			expErr:         true,
			expErrContains: "item not found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)

			var collID uint64
			var itemIDs []uint64
			if tc.setup != nil {
				collID, itemIDs = tc.setup(f)
			}

			msg := tc.msg(f, itemIDs)
			_, err := f.msgServer.RemoveItems(f.ctx, msg)
			if tc.expErr {
				require.Error(t, err)
				if tc.expErrContains != "" {
					require.Contains(t, err.Error(), tc.expErrContains)
				}
				return
			}
			require.NoError(t, err)
			if tc.check != nil {
				tc.check(t, f, collID, itemIDs)
			}
		})
	}
}

func TestRemoveItemsTTLRefund(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createTTLCollection(t, f.owner, 10100)
	id1 := f.addItem(t, collID, f.owner)
	id2 := f.addItem(t, collID, f.owner)

	_, err := f.msgServer.RemoveItems(f.ctx, &types.MsgRemoveItems{
		Creator: f.owner,
		Ids:     []uint64{id1, id2},
	})
	require.NoError(t, err)

	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, uint64(0), coll.ItemCount)
}
