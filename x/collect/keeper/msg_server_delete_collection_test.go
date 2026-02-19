package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestDeleteCollection(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture) uint64
		msg            func(f *testFixture, collID uint64) *types.MsgDeleteCollection
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture, collID uint64)
	}{
		{
			name: "owner deletes empty collection (deposit refunded)",
			setup: func(f *testFixture) uint64 {
				f.setBlockHeight(100)
				return f.createTTLCollection(t, f.owner, 10100)
			},
			msg: func(f *testFixture, collID uint64) *types.MsgDeleteCollection {
				return &types.MsgDeleteCollection{
					Creator: f.owner,
					Id:      collID,
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, collID uint64) {
				// Collection should be removed
				_, err := f.keeper.Collection.Get(f.ctx, collID)
				require.Error(t, err)

				// Owner index should be cleaned
				has, err := f.keeper.CollectionsByOwner.Has(f.ctx, collections.Join(f.owner, collID))
				require.NoError(t, err)
				require.False(t, has)
			},
		},
		{
			name: "owner deletes collection with items",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				f.addItem(t, collID, f.owner)
				f.addItem(t, collID, f.owner)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgDeleteCollection {
				return &types.MsgDeleteCollection{
					Creator: f.owner,
					Id:      collID,
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, collID uint64) {
				// Collection should be removed
				_, err := f.keeper.Collection.Get(f.ctx, collID)
				require.Error(t, err)

				// Items should be removed (iterate to check)
				var itemCount int
				f.keeper.ItemsByCollection.Walk(f.ctx,
					collections.NewPrefixedPairRange[uint64, uint64](collID),
					func(key collections.Pair[uint64, uint64]) (bool, error) {
						itemCount++
						return false, nil
					},
				)
				require.Equal(t, 0, itemCount)
			},
		},
		{
			name: "error: non-owner cannot delete",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			msg: func(f *testFixture, collID uint64) *types.MsgDeleteCollection {
				return &types.MsgDeleteCollection{
					Creator: f.member,
					Id:      collID,
				}
			},
			expErr:         true,
			expErrContains: "unauthorized",
		},
		{
			name: "error: collection not found",
			setup: func(f *testFixture) uint64 {
				return 999999 // non-existent
			},
			msg: func(f *testFixture, collID uint64) *types.MsgDeleteCollection {
				return &types.MsgDeleteCollection{
					Creator: f.owner,
					Id:      collID,
				}
			},
			expErr:         true,
			expErrContains: "collection not found",
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
			_, err := f.msgServer.DeleteCollection(f.ctx, msg)
			if tc.expErr {
				require.Error(t, err)
				if tc.expErrContains != "" {
					require.Contains(t, err.Error(), tc.expErrContains)
				}
				return
			}
			require.NoError(t, err)
			if tc.check != nil {
				tc.check(t, f, collID)
			}
		})
	}
}

func TestDeleteCollectionRefundsDeposit(t *testing.T) {
	f := initTestFixture(t)

	var refundCalled bool
	f.bankKeeper.sendCoinsFromModuleToAccountFn = func(_ context.Context, senderModule string, recipient sdk.AccAddress, amt sdk.Coins) error {
		refundCalled = true
		return nil
	}

	f.setBlockHeight(100)
	collID := f.createTTLCollection(t, f.owner, 10100)

	// Delete should trigger refund
	refundCalled = false
	_, err := f.msgServer.DeleteCollection(f.ctx, &types.MsgDeleteCollection{
		Creator: f.owner,
		Id:      collID,
	})
	require.NoError(t, err)
	require.True(t, refundCalled)
}
