package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// TestMoveCollectionStatusIndex_StatusTransition covers the status-change path
// (PENDING → ACTIVE) with pinned held constant — the path most state-machine
// transitions take.
func TestMoveCollectionStatusIndex_StatusTransition(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)
	active := int32(types.CollectionStatus_COLLECTION_STATUS_ACTIVE)
	hidden := int32(types.CollectionStatus_COLLECTION_STATUS_HIDDEN)

	// Seed the index under ACTIVE (createCollection already did this), then move
	// it to HIDDEN without touching pinned.
	require.NoError(t, f.keeper.MoveCollectionStatusIndex(f.ctx,
		types.CollectionStatus_COLLECTION_STATUS_ACTIVE, false,
		types.CollectionStatus_COLLECTION_STATUS_HIDDEN, false, collID))

	has, err := f.keeper.CollectionsByStatus.Has(f.ctx, collections.Join3(active, int32(1), collID))
	require.NoError(t, err)
	require.False(t, has, "old ACTIVE entry removed")
	has, err = f.keeper.CollectionsByStatus.Has(f.ctx, collections.Join3(hidden, int32(1), collID))
	require.NoError(t, err)
	require.True(t, has, "new HIDDEN entry present")
}

// TestMoveCollectionStatusIndex_NoOp verifies the short-circuit: when neither
// status nor pinned changes, Move is a no-op and the index is untouched.
func TestMoveCollectionStatusIndex_NoOp(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)
	active := int32(types.CollectionStatus_COLLECTION_STATUS_ACTIVE)

	// Move to the exact same (status, pinned) — must be a no-op.
	require.NoError(t, f.keeper.MoveCollectionStatusIndex(f.ctx,
		types.CollectionStatus_COLLECTION_STATUS_ACTIVE, false,
		types.CollectionStatus_COLLECTION_STATUS_ACTIVE, false, collID))

	has, err := f.keeper.CollectionsByStatus.Has(f.ctx, collections.Join3(active, int32(1), collID))
	require.NoError(t, err)
	require.True(t, has, "entry still present after no-op move")
}

// TestMoveCollectionStatusIndex_PinFlip covers the pin-flip path (the bug-fix
// scenario): pinned changes while status stays constant.
func TestMoveCollectionStatusIndex_PinFlip(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)
	active := int32(types.CollectionStatus_COLLECTION_STATUS_ACTIVE)

	// Pin: status unchanged, pinned false → true.
	require.NoError(t, f.keeper.MoveCollectionStatusIndex(f.ctx,
		types.CollectionStatus_COLLECTION_STATUS_ACTIVE, false,
		types.CollectionStatus_COLLECTION_STATUS_ACTIVE, true, collID))

	has, err := f.keeper.CollectionsByStatus.Has(f.ctx, collections.Join3(active, int32(1), collID))
	require.NoError(t, err)
	require.False(t, has, "unpinned-rank entry removed on pin")
	has, err = f.keeper.CollectionsByStatus.Has(f.ctx, collections.Join3(active, int32(0), collID))
	require.NoError(t, err)
	require.True(t, has, "pinned-rank entry present after pin")

	// Unpin: back to the original rank.
	require.NoError(t, f.keeper.MoveCollectionStatusIndex(f.ctx,
		types.CollectionStatus_COLLECTION_STATUS_ACTIVE, true,
		types.CollectionStatus_COLLECTION_STATUS_ACTIVE, false, collID))

	has, err = f.keeper.CollectionsByStatus.Has(f.ctx, collections.Join3(active, int32(0), collID))
	require.NoError(t, err)
	require.False(t, has, "pinned-rank entry removed on unpin")
	has, err = f.keeper.CollectionsByStatus.Has(f.ctx, collections.Join3(active, int32(1), collID))
	require.NoError(t, err)
	require.True(t, has, "unpinned-rank entry restored after unpin")
}

// TestMoveCollectionStatusIndex_StatusAndPin covers the combined path where
// both status and pinned change in a single transition.
func TestMoveCollectionStatusIndex_StatusAndPin(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)
	active := int32(types.CollectionStatus_COLLECTION_STATUS_ACTIVE)
	hidden := int32(types.CollectionStatus_COLLECTION_STATUS_HIDDEN)

	// Move ACTIVE+unpinned → HIDDEN+pinned in one go.
	require.NoError(t, f.keeper.MoveCollectionStatusIndex(f.ctx,
		types.CollectionStatus_COLLECTION_STATUS_ACTIVE, false,
		types.CollectionStatus_COLLECTION_STATUS_HIDDEN, true, collID))

	has, err := f.keeper.CollectionsByStatus.Has(f.ctx, collections.Join3(active, int32(1), collID))
	require.NoError(t, err)
	require.False(t, has, "old ACTIVE/unpinned entry removed")
	has, err = f.keeper.CollectionsByStatus.Has(f.ctx, collections.Join3(hidden, int32(0), collID))
	require.NoError(t, err)
	require.True(t, has, "new HIDDEN/pinned entry present")
}

// TestRemoveCollectionStatusIndex_Idempotent verifies that removing an entry
// that isn't there doesn't error (the KeySet.Remove semantics the helpers rely
// on for the "before" state of a transition).
func TestRemoveCollectionStatusIndex_Idempotent(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)
	active := int32(types.CollectionStatus_COLLECTION_STATUS_ACTIVE)

	// Remove once (entry exists), then remove again (entry gone) — neither panics.
	f.keeper.RemoveCollectionStatusIndex(f.ctx, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, false, collID)
	f.keeper.RemoveCollectionStatusIndex(f.ctx, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, false, collID)

	has, err := f.keeper.CollectionsByStatus.Has(f.ctx, collections.Join3(active, int32(1), collID))
	require.NoError(t, err)
	require.False(t, has, "entry gone after remove")
}

// TestStatusIndexConsistencyInvariant_PinnedRoundTrip exercises the invariant
// across a full pin → unpin round-trip through the public helpers, mirroring
// what PinCollection/UnpinCollection do internally.
func TestStatusIndexConsistencyInvariant_PinnedRoundTrip(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)
	invariant := keeper.StatusIndexConsistencyInvariant(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Pin via the helper (as PinCollection does).
	require.NoError(t, f.keeper.MoveCollectionStatusIndex(f.ctx,
		types.CollectionStatus_COLLECTION_STATUS_ACTIVE, false,
		types.CollectionStatus_COLLECTION_STATUS_ACTIVE, true, collID))
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	coll.Pinned = true
	require.NoError(t, f.keeper.Collection.Set(f.ctx, collID, coll))
	_, broken := invariant(sdkCtx)
	require.False(t, broken, "invariant holds after pin")

	// Unpin via the helper (as UnpinCollection does).
	require.NoError(t, f.keeper.MoveCollectionStatusIndex(f.ctx,
		types.CollectionStatus_COLLECTION_STATUS_ACTIVE, true,
		types.CollectionStatus_COLLECTION_STATUS_ACTIVE, false, collID))
	coll, _ = f.keeper.Collection.Get(f.ctx, collID)
	coll.Pinned = false
	require.NoError(t, f.keeper.Collection.Set(f.ctx, collID, coll))
	_, broken = invariant(sdkCtx)
	require.False(t, broken, "invariant holds after unpin")
}