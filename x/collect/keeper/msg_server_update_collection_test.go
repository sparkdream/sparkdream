package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestUpdateCollection(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture) uint64
		msg            func(f *testFixture, collID uint64) *types.MsgUpdateCollection
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture, collID uint64)
	}{
		{
			name: "owner updates name, description, and tags",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			msg: func(f *testFixture, collID uint64) *types.MsgUpdateCollection {
				return &types.MsgUpdateCollection{
					Creator:     f.owner,
					Id:          collID,
					Type:        types.CollectionType_COLLECTION_TYPE_MIXED,
					Name:        "updated-name",
					Description: "updated-desc",
					Tags:        []string{"tag1", "tag2"},
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, collID uint64) {
				coll, err := f.keeper.Collection.Get(f.ctx, collID)
				require.NoError(t, err)
				require.Equal(t, "updated-name", coll.Name)
				require.Equal(t, "updated-desc", coll.Description)
				require.Equal(t, []string{"tag1", "tag2"}, coll.Tags)
			},
		},
		{
			name: "member converts TTL to permanent (held deposits burned via BurnSPARK)",
			setup: func(f *testFixture) uint64 {
				f.setBlockHeight(100)
				collID := f.createTTLCollection(t, f.owner, 10100)
				// Add an item to check item deposit total handling
				f.addItem(t, collID, f.owner)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgUpdateCollection {
				return &types.MsgUpdateCollection{
					Creator: f.owner,
					Id:      collID,
					Type:    types.CollectionType_COLLECTION_TYPE_MIXED,
					Name:    "now-permanent",
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, collID uint64) {
				coll, err := f.keeper.Collection.Get(f.ctx, collID)
				require.NoError(t, err)
				require.Equal(t, int64(0), coll.ExpiresAt)
				require.True(t, coll.DepositBurned)
			},
		},
		{
			name: "error: non-owner cannot update",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			msg: func(f *testFixture, collID uint64) *types.MsgUpdateCollection {
				return &types.MsgUpdateCollection{
					Creator: f.member,
					Id:      collID,
					Type:    types.CollectionType_COLLECTION_TYPE_MIXED,
					Name:    "hacked",
				}
			},
			expErr:         true,
			expErrContains: "unauthorized",
		},
		{
			name: "error: immutable collection",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				// Set immutable directly
				coll, _ := f.keeper.Collection.Get(f.ctx, collID)
				coll.Immutable = true
				f.keeper.Collection.Set(f.ctx, collID, coll)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgUpdateCollection {
				return &types.MsgUpdateCollection{
					Creator: f.owner,
					Id:      collID,
					Type:    types.CollectionType_COLLECTION_TYPE_MIXED,
					Name:    "cannot-update",
				}
			},
			expErr:         true,
			expErrContains: "immutable",
		},
		{
			name: "error: non-member TTL extension beyond max",
			setup: func(f *testFixture) uint64 {
				f.setBlockHeight(100)
				collID := f.createPendingCollection(t)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgUpdateCollection {
				coll, _ := f.keeper.Collection.Get(f.ctx, collID)
				return &types.MsgUpdateCollection{
					Creator:   f.nonMember,
					Id:        collID,
					Type:      coll.Type,
					Name:      coll.Name,
					ExpiresAt: coll.CreatedAt + 500000, // 500000 > 432000 max_non_member_ttl
				}
			},
			expErr:         true,
			expErrContains: "non-member TTL exceeds max",
		},
		{
			name: "error: permanent cannot set expires_at",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner) // permanent (ExpiresAt=0)
			},
			msg: func(f *testFixture, collID uint64) *types.MsgUpdateCollection {
				return &types.MsgUpdateCollection{
					Creator:   f.owner,
					Id:        collID,
					Type:      types.CollectionType_COLLECTION_TYPE_MIXED,
					Name:      "still-permanent",
					ExpiresAt: 99999,
				}
			},
			expErr:         true,
			expErrContains: "permanent collection cannot set expires_at",
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
			_, err := f.msgServer.UpdateCollection(f.ctx, msg)
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

func TestUpdateCollectionBurnDeposit(t *testing.T) {
	f := initTestFixture(t)

	var burnCalled bool
	f.bankKeeper.burnCoinsFn = func(_ context.Context, moduleName string, amt sdk.Coins) error {
		burnCalled = true
		return nil
	}

	f.setBlockHeight(100)
	collID := f.createTTLCollection(t, f.owner, 10100)

	// Verify deposit is escrowed (not burned yet)
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.False(t, coll.DepositBurned)
	require.True(t, coll.DepositAmount.Equal(math.NewInt(1000000)))

	burnCalled = false
	// Convert TTL -> permanent
	_, err = f.msgServer.UpdateCollection(f.ctx, &types.MsgUpdateCollection{
		Creator: f.owner,
		Id:      collID,
		Type:    types.CollectionType_COLLECTION_TYPE_MIXED,
		Name:    "permanent-now",
	})
	require.NoError(t, err)
	require.True(t, burnCalled)

	coll, err = f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.True(t, coll.DepositBurned)
	require.Equal(t, int64(0), coll.ExpiresAt)
}
