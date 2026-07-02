package keeper

import "sparkdream/x/collect/types"

// maxPublicCollectionsLimit caps a single page of public-collection results.
// Both PublicCollections and PublicCollectionsByType walk the full ACTIVE+PUBLIC
// set into memory to apply pinned-first ordering before slicing, so an
// unbounded caller-supplied limit must be clamped — both to bound the response
// size and to keep offset+limit from overflowing in paginate.
const maxPublicCollectionsLimit uint64 = 100

// pinnedFirst returns a new slice ordering pinned collections ahead of unpinned
// ones, preserving the input order within each group (a stable partition). The
// input is expected to already be in collection-ID order from the index walk, so
// the result is pinned-by-ID followed by unpinned-by-ID.
func pinnedFirst(in []types.Collection) []types.Collection {
	if len(in) == 0 {
		return in
	}
	ordered := make([]types.Collection, 0, len(in))
	for _, c := range in {
		if c.Pinned {
			ordered = append(ordered, c)
		}
	}
	for _, c := range in {
		if !c.Pinned {
			ordered = append(ordered, c)
		}
	}
	return ordered
}

// paginate applies offset/limit slicing to an already-ordered slice. It is
// overflow-safe: offset+limit is computed in a way that cannot wrap, so even an
// unclamped limit near math.MaxUint64 yields a valid slice rather than panicking
// on out-of-range bounds.
func paginate(in []types.Collection, offset, limit uint64) []types.Collection {
	n := uint64(len(in))
	if offset >= n {
		return nil
	}
	// Remaining elements after offset; limit can be at most this many without
	// risking offset+limit overflowing past n.
	if limit > n-offset {
		limit = n - offset
	}
	return in[offset : offset+limit]
}
