package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/collect/types"
)

// CollectionsByStatus is keyed (status, pinned-rank, collectionID). Encoding the
// pinned marker as the second component lets a status-prefixed walk yield
// pinned-first results in collection-ID order natively, with no in-memory sort
// and without losing early pagination — see PublicCollections.
//
// Every status *or* pinned transition must route through the helpers below so
// the denormalized index can never drift from the collection's own fields. The
// index keys on two mutable fields (Collection.Status and Collection.Pinned), so
// pin/unpin must re-point it just as status changes do.

// pinnedStatusRank orders pinned collections (rank 0) ahead of unpinned ones
// (rank 1) within a status group. Lower sorts first under the ascending walk.
func pinnedStatusRank(pinned bool) int32 {
	if pinned {
		return 0
	}
	return 1
}

// collectionsByStatusKey builds the (status, pinned-rank, id) index key.
func collectionsByStatusKey(status types.CollectionStatus, pinned bool, id uint64) collections.Triple[int32, int32, uint64] {
	return collections.Join3(int32(status), pinnedStatusRank(pinned), id)
}

// AddCollectionStatusIndex indexes a collection under its current status and
// pinned marker. Used for creation, genesis import, and as the "add" half of a
// transition.
func (k Keeper) AddCollectionStatusIndex(ctx context.Context, status types.CollectionStatus, pinned bool, id uint64) error {
	return k.CollectionsByStatus.Set(ctx, collectionsByStatusKey(status, pinned, id))
}

// RemoveCollectionStatusIndex drops the index entry for the given status and
// pinned marker. Callers must pass the status/pinned the entry was written with
// (the values *before* any mutation).
func (k Keeper) RemoveCollectionStatusIndex(ctx context.Context, status types.CollectionStatus, pinned bool, id uint64) {
	_ = k.CollectionsByStatus.Remove(ctx, collectionsByStatusKey(status, pinned, id))
}

// MoveCollectionStatusIndex re-points the index entry from a prior (status,
// pinned) to a new one. This is the single chokepoint every status or pinned
// transition goes through. It is a no-op when neither indexed field changed.
func (k Keeper) MoveCollectionStatusIndex(ctx context.Context, oldStatus types.CollectionStatus, oldPinned bool, newStatus types.CollectionStatus, newPinned bool, id uint64) error {
	if oldStatus == newStatus && oldPinned == newPinned {
		return nil
	}
	k.RemoveCollectionStatusIndex(ctx, oldStatus, oldPinned, id)
	return k.AddCollectionStatusIndex(ctx, newStatus, newPinned, id)
}
