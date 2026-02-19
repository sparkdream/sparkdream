package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestAddItems(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture) uint64
		msg            func(f *testFixture, collID uint64) *types.MsgAddItems
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture, collID uint64, resp *types.MsgAddItemsResponse)
	}{
		{
			name: "batch add multiple items",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			msg: func(f *testFixture, collID uint64) *types.MsgAddItems {
				return &types.MsgAddItems{
					Creator:      f.owner,
					CollectionId: collID,
					Items: []types.AddItemEntry{
						{Title: "item-a"},
						{Title: "item-b"},
						{Title: "item-c"},
					},
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, collID uint64, resp *types.MsgAddItemsResponse) {
				require.Len(t, resp.Ids, 3)

				// All items stored
				for i, id := range resp.Ids {
					item, err := f.keeper.Item.Get(f.ctx, id)
					require.NoError(t, err)
					require.Equal(t, collID, item.CollectionId)
					require.Equal(t, uint64(i), item.Position)
				}

				// Collection item_count incremented
				coll, err := f.keeper.Collection.Get(f.ctx, collID)
				require.NoError(t, err)
				require.Equal(t, uint64(3), coll.ItemCount)
			},
		},
		{
			name: "error: batch exceeds max_batch_size",
			setup: func(f *testFixture) uint64 {
				// Set max_batch_size to 2
				params, _ := f.keeper.Params.Get(f.ctx)
				params.MaxBatchSize = 2
				f.keeper.Params.Set(f.ctx, params)
				return f.createCollection(t, f.owner)
			},
			msg: func(f *testFixture, collID uint64) *types.MsgAddItems {
				return &types.MsgAddItems{
					Creator:      f.owner,
					CollectionId: collID,
					Items: []types.AddItemEntry{
						{Title: "a"},
						{Title: "b"},
						{Title: "c"},
					},
				}
			},
			expErr:         true,
			expErrContains: "batch size exceeds max batch size",
		},
		{
			name: "error: empty items list",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			msg: func(f *testFixture, collID uint64) *types.MsgAddItems {
				return &types.MsgAddItems{
					Creator:      f.owner,
					CollectionId: collID,
					Items:        []types.AddItemEntry{},
				}
			},
			expErr:         true,
			expErrContains: "empty batch",
		},
		{
			name: "error: total items would exceed max",
			setup: func(f *testFixture) uint64 {
				params, _ := f.keeper.Params.Get(f.ctx)
				params.MaxItemsPerCollection = 2
				f.keeper.Params.Set(f.ctx, params)
				collID := f.createCollection(t, f.owner)
				f.addItem(t, collID, f.owner) // 1 item
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgAddItems {
				return &types.MsgAddItems{
					Creator:      f.owner,
					CollectionId: collID,
					Items: []types.AddItemEntry{
						{Title: "x"},
						{Title: "y"},
					},
				}
			},
			expErr:         true,
			expErrContains: "max items per collection",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)

			var collID uint64
			if tc.setup != nil {
				collID = tc.setup(f)
			}

			msg := tc.msg(f, collID)
			resp, err := f.msgServer.AddItems(f.ctx, msg)
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
				tc.check(t, f, collID, resp)
			}
		})
	}
}

func TestAddItemsTTLDeposit(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	collID := f.createTTLCollection(t, f.owner, 10100)

	resp, err := f.msgServer.AddItems(f.ctx, &types.MsgAddItems{
		Creator:      f.owner,
		CollectionId: collID,
		Items: []types.AddItemEntry{
			{Title: "a"},
			{Title: "b"},
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Ids, 2)

	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, uint64(2), coll.ItemCount)

	params, _ := f.keeper.Params.Get(f.ctx)
	expectedDeposit := params.PerItemDeposit.MulRaw(2)
	require.True(t, coll.ItemDepositTotal.Equal(expectedDeposit))
}
