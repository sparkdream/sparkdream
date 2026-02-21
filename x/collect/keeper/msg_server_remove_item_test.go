package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestRemoveItem(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture) (collID uint64, itemID uint64)
		msg            func(f *testFixture, itemID uint64) *types.MsgRemoveItem
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture, collID uint64, itemID uint64)
	}{
		{
			name: "owner removes item, positions compacted",
			setup: func(f *testFixture) (uint64, uint64) {
				collID := f.createCollection(t, f.owner)
				f.addItem(t, collID, f.owner)           // item at pos 0
				itemID := f.addItem(t, collID, f.owner) // item at pos 1
				f.addItem(t, collID, f.owner)           // item at pos 2
				return collID, itemID
			},
			msg: func(f *testFixture, itemID uint64) *types.MsgRemoveItem {
				return &types.MsgRemoveItem{
					Creator: f.owner,
					Id:      itemID,
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, collID uint64, itemID uint64) {
				// Item should be removed
				_, err := f.keeper.Item.Get(f.ctx, itemID)
				require.Error(t, err)

				// Collection item_count decremented (was 3, now 2)
				coll, err := f.keeper.Collection.Get(f.ctx, collID)
				require.NoError(t, err)
				require.Equal(t, uint64(2), coll.ItemCount)
			},
		},
		{
			name: "TTL collection refunds per_item_deposit",
			setup: func(f *testFixture) (uint64, uint64) {
				f.setBlockHeight(100)
				collID := f.createTTLCollection(t, f.owner, 10100)
				itemID := f.addItem(t, collID, f.owner)
				return collID, itemID
			},
			msg: func(f *testFixture, itemID uint64) *types.MsgRemoveItem {
				return &types.MsgRemoveItem{
					Creator: f.owner,
					Id:      itemID,
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, collID uint64, itemID uint64) {
				coll, err := f.keeper.Collection.Get(f.ctx, collID)
				require.NoError(t, err)
				require.Equal(t, uint64(0), coll.ItemCount)
				require.True(t, coll.ItemDepositTotal.Equal(math.ZeroInt()))
			},
		},
		{
			name: "error: item not found",
			setup: func(f *testFixture) (uint64, uint64) {
				return 0, 999999
			},
			msg: func(f *testFixture, itemID uint64) *types.MsgRemoveItem {
				return &types.MsgRemoveItem{
					Creator: f.owner,
					Id:      itemID,
				}
			},
			expErr:         true,
			expErrContains: "item not found",
		},
		{
			name: "error: not authorized (non-owner, non-collaborator)",
			setup: func(f *testFixture) (uint64, uint64) {
				collID := f.createCollection(t, f.owner)
				itemID := f.addItem(t, collID, f.owner)
				return collID, itemID
			},
			msg: func(f *testFixture, itemID uint64) *types.MsgRemoveItem {
				return &types.MsgRemoveItem{
					Creator: f.member,
					Id:      itemID,
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
				coll, _ := f.keeper.Collection.Get(f.ctx, collID)
				coll.Immutable = true
				f.keeper.Collection.Set(f.ctx, collID, coll)
				return collID, itemID
			},
			msg: func(f *testFixture, itemID uint64) *types.MsgRemoveItem {
				return &types.MsgRemoveItem{
					Creator: f.owner,
					Id:      itemID,
				}
			},
			expErr:         true,
			expErrContains: "immutable",
		},
		{
			name: "error: pending sponsorship blocks remove",
			setup: func(f *testFixture) (uint64, uint64) {
				collID := f.createCollection(t, f.owner)
				itemID := f.addItem(t, collID, f.owner)
				f.keeper.SponsorshipRequest.Set(f.ctx, collID, types.SponsorshipRequest{
					CollectionId:      collID,
					Requester:         f.owner,
					CollectionDeposit: math.NewInt(1000000),
					ItemDepositTotal:  math.ZeroInt(),
					ExpiresAt:         99999,
				})
				return collID, itemID
			},
			msg: func(f *testFixture, itemID uint64) *types.MsgRemoveItem {
				return &types.MsgRemoveItem{
					Creator: f.owner,
					Id:      itemID,
				}
			},
			expErr:         true,
			expErrContains: "sponsorship request is pending",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)

			var collID, itemID uint64
			if tc.setup != nil {
				collID, itemID = tc.setup(f)
			}

			msg := tc.msg(f, itemID)
			_, err := f.msgServer.RemoveItem(f.ctx, msg)
			if tc.expErr {
				require.Error(t, err)
				if tc.expErrContains != "" {
					require.Contains(t, err.Error(), tc.expErrContains)
				}
				return
			}
			require.NoError(t, err)
			if tc.check != nil {
				tc.check(t, f, collID, itemID)
			}
		})
	}
}

func TestRemoveItemRefund(t *testing.T) {
	f := initTestFixture(t)

	var refundCalled bool
	f.bankKeeper.sendCoinsFromModuleToAccountFn = func(_ context.Context, senderModule string, recipient sdk.AccAddress, amt sdk.Coins) error {
		refundCalled = true
		return nil
	}

	f.setBlockHeight(100)
	collID := f.createTTLCollection(t, f.owner, 10100)
	itemID := f.addItem(t, collID, f.owner)

	refundCalled = false
	_, err := f.msgServer.RemoveItem(f.ctx, &types.MsgRemoveItem{
		Creator: f.owner,
		Id:      itemID,
	})
	require.NoError(t, err)
	require.True(t, refundCalled)
}
