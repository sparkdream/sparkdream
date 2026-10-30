package types

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
)

// RemoveIndexed deletes key from an IndexedMap, treating a record that is
// already gone as removed.
//
// IndexedMap.Remove reads the stored value to drop its index entries, so on a
// missing key it returns collections.ErrNotFound, where a plain Map.Remove
// silently succeeds. Callers that only mean "make sure it is gone" use this
// and still see every other store error.
func RemoveIndexed[K, V, I any](ctx context.Context, m *collections.IndexedMap[K, V, I], key K) error {
	if err := m.Remove(ctx, key); err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	return nil
}
