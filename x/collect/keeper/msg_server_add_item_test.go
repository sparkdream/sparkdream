package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestAddItem(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture) uint64
		msg            func(f *testFixture, collID uint64) *types.MsgAddItem
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture, collID uint64, resp *types.MsgAddItemResponse)
	}{
		{
			name: "owner adds item at default position",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			msg: func(f *testFixture, collID uint64) *types.MsgAddItem {
				return &types.MsgAddItem{
					Creator:      f.owner,
					CollectionId: collID,
					Title:        "first-item",
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, collID uint64, resp *types.MsgAddItemResponse) {
				item, err := f.keeper.Item.Get(f.ctx, resp.Id)
				require.NoError(t, err)
				require.Equal(t, "first-item", item.Title)
				require.Equal(t, uint64(0), item.Position) // first item, position=0

				// Collection item_count incremented
				coll, err := f.keeper.Collection.Get(f.ctx, collID)
				require.NoError(t, err)
				require.Equal(t, uint64(1), coll.ItemCount)
			},
		},
		{
			name: "editor collaborator adds item",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				f.addCollaborator(t, collID, f.owner, f.member, types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgAddItem {
				return &types.MsgAddItem{
					Creator:      f.member,
					CollectionId: collID,
					Title:        "collab-item",
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, collID uint64, resp *types.MsgAddItemResponse) {
				item, err := f.keeper.Item.Get(f.ctx, resp.Id)
				require.NoError(t, err)
				require.Equal(t, "collab-item", item.Title)
				require.Equal(t, f.member, item.AddedBy)
			},
		},
		{
			name: "error: non-collaborator member cannot add",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				// member is not a collaborator, so no write access
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgAddItem {
				return &types.MsgAddItem{
					Creator:      f.member,
					CollectionId: collID,
					Title:        "cannot-add",
				}
			},
			expErr:         true,
			expErrContains: "unauthorized",
		},
		{
			name: "error: immutable collection rejects add",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				coll, _ := f.keeper.Collection.Get(f.ctx, collID)
				coll.Immutable = true
				f.keeper.Collection.Set(f.ctx, collID, coll)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgAddItem {
				return &types.MsgAddItem{
					Creator:      f.owner,
					CollectionId: collID,
					Title:        "no-add",
				}
			},
			expErr:         true,
			expErrContains: "immutable",
		},
		{
			name: "error: max items exceeded",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				// Set item_count to max directly
				params, _ := f.keeper.Params.Get(f.ctx)
				coll, _ := f.keeper.Collection.Get(f.ctx, collID)
				coll.ItemCount = uint64(params.MaxItemsPerCollection)
				f.keeper.Collection.Set(f.ctx, collID, coll)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgAddItem {
				return &types.MsgAddItem{
					Creator:      f.owner,
					CollectionId: collID,
					Title:        "over-limit",
				}
			},
			expErr:         true,
			expErrContains: "max items per collection",
		},
		{
			name: "error: pending sponsorship blocks add",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				// Store a SponsorshipRequest to block item operations
				f.keeper.SponsorshipRequest.Set(f.ctx, collID, types.SponsorshipRequest{
					CollectionId:      collID,
					Requester:         f.owner,
					CollectionDeposit: math.NewInt(1000000),
					ItemDepositTotal:  math.ZeroInt(),
					ExpiresAt:         99999,
				})
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgAddItem {
				return &types.MsgAddItem{
					Creator:      f.owner,
					CollectionId: collID,
					Title:        "blocked",
				}
			},
			expErr:         true,
			expErrContains: "sponsorship request is pending",
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
			resp, err := f.msgServer.AddItem(f.ctx, msg)
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

func TestAddItemPositionAppend(t *testing.T) {
	f := initTestFixture(t)
	collID := f.createCollection(t, f.owner)

	// Add first item (position 0 appends when ItemCount=0)
	resp1, err := f.msgServer.AddItem(f.ctx, &types.MsgAddItem{
		Creator:      f.owner,
		CollectionId: collID,
		Title:        "item-0",
		Position:     1000, // >= ItemCount → append at end
	})
	require.NoError(t, err)

	// Add second item (position >= ItemCount → append at end)
	resp2, err := f.msgServer.AddItem(f.ctx, &types.MsgAddItem{
		Creator:      f.owner,
		CollectionId: collID,
		Title:        "item-1",
		Position:     1000, // >= ItemCount → append at end
	})
	require.NoError(t, err)

	item1, _ := f.keeper.Item.Get(f.ctx, resp1.Id)
	item2, _ := f.keeper.Item.Get(f.ctx, resp2.Id)
	require.Equal(t, uint64(0), item1.Position)
	require.Equal(t, uint64(1), item2.Position)

	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	require.Equal(t, uint64(2), coll.ItemCount)
}

func TestAddItemTTLCollectionEscrows(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	collID := f.createTTLCollection(t, f.owner, 10100)

	resp, err := f.msgServer.AddItem(f.ctx, &types.MsgAddItem{
		Creator:      f.owner,
		CollectionId: collID,
		Title:        "ttl-item",
	})
	require.NoError(t, err)

	// Check item stored
	_, err = f.keeper.Item.Get(f.ctx, resp.Id)
	require.NoError(t, err)

	// Check collection item_deposit_total updated
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	params, _ := f.keeper.Params.Get(f.ctx)
	require.True(t, coll.ItemDepositTotal.Equal(params.PerItemDeposit))
}
