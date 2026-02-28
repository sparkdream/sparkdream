package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

func TestCollectionCounterInvariant_Passing(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Create a collection (seq will be > collection ID)
	f.createCollection(t, f.owner)

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	msg, broken := keeper.CollectionCounterInvariant(f.keeper)(sdkCtx)
	require.False(t, broken, msg)
}

func TestCollectionCounterInvariant_Broken(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Create a collection to advance sequence to 1
	collID := f.createCollection(t, f.owner)

	// Manually set collection with an ID >= sequence (simulate corruption)
	// The sequence is now at 1 (next would be 1), but collID is 0.
	// Manually insert a collection with ID = 999 (way beyond sequence).
	coll := types.Collection{
		Id:               999,
		Owner:            f.owner,
		Name:             "corrupt",
		Status:           types.CollectionStatus_COLLECTION_STATUS_ACTIVE,
		DepositAmount:    math.ZeroInt(),
		ItemDepositTotal: math.ZeroInt(),
	}
	err := f.keeper.Collection.Set(f.ctx, 999, coll)
	require.NoError(t, err)
	_ = collID

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	msg, broken := keeper.CollectionCounterInvariant(f.keeper)(sdkCtx)
	require.True(t, broken, msg)
	require.Contains(t, msg, "collection counter violations")
}

func TestItemCounterInvariant_Passing(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Create a collection and add an item
	collID := f.createCollection(t, f.owner)
	f.addItem(t, collID, f.owner)

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	msg, broken := keeper.ItemCounterInvariant(f.keeper)(sdkCtx)
	require.False(t, broken, msg)
}

func TestItemCounterInvariant_Broken(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)
	_ = collID

	// Manually insert an item with ID = 999 (beyond sequence)
	item := types.Item{
		Id:           999,
		CollectionId: collID,
		Title:        "corrupt-item",
		Status:       types.ItemStatus_ITEM_STATUS_ACTIVE,
	}
	err := f.keeper.Item.Set(f.ctx, 999, item)
	require.NoError(t, err)

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	msg, broken := keeper.ItemCounterInvariant(f.keeper)(sdkCtx)
	require.True(t, broken, msg)
	require.Contains(t, msg, "item counter violations")
}

func TestItemCollectionReferenceInvariant_Passing(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)
	f.addItem(t, collID, f.owner)

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	msg, broken := keeper.ItemCollectionReferenceInvariant(f.keeper)(sdkCtx)
	require.False(t, broken, msg)
}

func TestItemCollectionReferenceInvariant_Broken(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Insert item that references a non-existent collection
	item := types.Item{
		Id:           0,
		CollectionId: 9999, // doesn't exist
		Title:        "orphan-item",
		Status:       types.ItemStatus_ITEM_STATUS_ACTIVE,
	}
	err := f.keeper.Item.Set(f.ctx, 0, item)
	require.NoError(t, err)
	// Also advance the item sequence so the invariant doesn't fail on counter first
	f.keeper.ItemSeq.Next(f.ctx) //nolint:errcheck

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	msg, broken := keeper.ItemCollectionReferenceInvariant(f.keeper)(sdkCtx)
	require.True(t, broken, msg)
	require.Contains(t, msg, "non-existent collection")
}

func TestHideRecordConsistencyInvariant_Passing(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// No hide records => passing
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	msg, broken := keeper.HideRecordConsistencyInvariant(f.keeper)(sdkCtx)
	require.False(t, broken, msg)
}

func TestHideRecordConsistencyInvariant_Broken(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Create an ACTIVE collection (not HIDDEN)
	collID := f.createCollection(t, f.owner)

	// Insert an unresolved hide record referencing this (non-hidden) collection
	hr := types.HideRecord{
		Id:         0,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		TargetId:   collID,
		Resolved:   false,
	}
	err := f.keeper.HideRecord.Set(f.ctx, 0, hr)
	require.NoError(t, err)
	f.keeper.HideRecordSeq.Next(f.ctx) //nolint:errcheck

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	msg, broken := keeper.HideRecordConsistencyInvariant(f.keeper)(sdkCtx)
	require.True(t, broken, msg)
	require.Contains(t, msg, "expected HIDDEN")
}

func TestHideRecordConsistencyInvariant_ResolvedSkipped(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Create an ACTIVE collection (not HIDDEN)
	collID := f.createCollection(t, f.owner)

	// Insert a resolved hide record - should be skipped
	hr := types.HideRecord{
		Id:         0,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		TargetId:   collID,
		Resolved:   true,
	}
	err := f.keeper.HideRecord.Set(f.ctx, 0, hr)
	require.NoError(t, err)

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	msg, broken := keeper.HideRecordConsistencyInvariant(f.keeper)(sdkCtx)
	require.False(t, broken, msg)
}

func TestStatusIndexConsistencyInvariant_Passing(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// createCollection sets status index properly
	f.createCollection(t, f.owner)

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	msg, broken := keeper.StatusIndexConsistencyInvariant(f.keeper)(sdkCtx)
	require.False(t, broken, msg)
}

func TestStatusIndexConsistencyInvariant_Broken(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Manually insert a collection WITHOUT setting the status index
	coll := types.Collection{
		Id:               0,
		Owner:            f.owner,
		Name:             "no-index",
		Status:           types.CollectionStatus_COLLECTION_STATUS_ACTIVE,
		DepositAmount:    math.ZeroInt(),
		ItemDepositTotal: math.ZeroInt(),
	}
	err := f.keeper.Collection.Set(f.ctx, 0, coll)
	require.NoError(t, err)
	f.keeper.CollectionSeq.Next(f.ctx) //nolint:errcheck

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	msg, broken := keeper.StatusIndexConsistencyInvariant(f.keeper)(sdkCtx)
	require.True(t, broken, msg)
	require.Contains(t, msg, "missing from CollectionsByStatus index")
}

func TestEmptyStoreInvariants(t *testing.T) {
	f := initTestFixture(t)

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// All invariants should pass on empty store
	tests := []struct {
		name      string
		invariant sdk.Invariant
	}{
		{"CollectionCounter", keeper.CollectionCounterInvariant(f.keeper)},
		{"ItemCounter", keeper.ItemCounterInvariant(f.keeper)},
		{"ItemCollectionReference", keeper.ItemCollectionReferenceInvariant(f.keeper)},
		{"HideRecordConsistency", keeper.HideRecordConsistencyInvariant(f.keeper)},
		{"StatusIndexConsistency", keeper.StatusIndexConsistencyInvariant(f.keeper)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg, broken := tc.invariant(sdkCtx)
			require.False(t, broken, msg)
		})
	}
}
