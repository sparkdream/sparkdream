package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

func TestIterateUsedNullifiers(t *testing.T) {
	f := initFixture(t)

	// Record several nullifiers across domains
	require.NoError(t, f.keeper.RecordNullifier(f.ctx, 1, 10, "null_a", 100))
	require.NoError(t, f.keeper.RecordNullifier(f.ctx, 1, 20, "null_b", 200))
	require.NoError(t, f.keeper.RecordNullifier(f.ctx, 2, 10, "null_c", 300))

	var collected []types.UsedNullifier
	err := f.keeper.IterateUsedNullifiers(f.ctx, func(n types.UsedNullifier) bool {
		collected = append(collected, n)
		return false
	})
	require.NoError(t, err)
	require.Len(t, collected, 3)
}

func TestIterateUsedNullifiersEarlyStop(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.RecordNullifier(f.ctx, 1, 10, "n1", 1))
	require.NoError(t, f.keeper.RecordNullifier(f.ctx, 1, 20, "n2", 2))
	require.NoError(t, f.keeper.RecordNullifier(f.ctx, 1, 30, "n3", 3))

	var count int
	err := f.keeper.IterateUsedNullifiers(f.ctx, func(_ types.UsedNullifier) bool {
		count++
		return count >= 2
	})
	require.NoError(t, err)
	require.Equal(t, 2, count)
}

func TestIteratePendingNullifiers(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.RecordPendingNullifier(f.ctx, "pn1"))
	require.NoError(t, f.keeper.RecordPendingNullifier(f.ctx, "pn2"))
	require.NoError(t, f.keeper.RecordPendingNullifier(f.ctx, "pn3"))

	var collected []string
	err := f.keeper.IteratePendingNullifiers(f.ctx, func(hex string) bool {
		collected = append(collected, hex)
		return false
	})
	require.NoError(t, err)
	require.Len(t, collected, 3)
}

func TestIteratePendingNullifiersEarlyStop(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.RecordPendingNullifier(f.ctx, "pn1"))
	require.NoError(t, f.keeper.RecordPendingNullifier(f.ctx, "pn2"))
	require.NoError(t, f.keeper.RecordPendingNullifier(f.ctx, "pn3"))

	var count int
	err := f.keeper.IteratePendingNullifiers(f.ctx, func(_ string) bool {
		count++
		return count >= 1
	})
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestNullifierDomainIsolation(t *testing.T) {
	f := initFixture(t)

	// Same nullifier hex in different domains should be independent
	require.NoError(t, f.keeper.RecordNullifier(f.ctx, 1, 0, "same_null", 10))
	require.NoError(t, f.keeper.RecordNullifier(f.ctx, 2, 0, "same_null", 20))

	n1, found := f.keeper.GetUsedNullifier(f.ctx, 1, 0, "same_null")
	require.True(t, found)
	require.Equal(t, int64(10), n1.UsedAtHeight)

	n2, found := f.keeper.GetUsedNullifier(f.ctx, 2, 0, "same_null")
	require.True(t, found)
	require.Equal(t, int64(20), n2.UsedAtHeight)
}

func TestPruneEpochScopedNullifiersEmpty(t *testing.T) {
	f := initFixture(t)

	// Prune on empty state should not error
	err := f.keeper.PruneEpochScopedNullifiers(f.ctx, 100)
	require.NoError(t, err)
}

func TestPruneEpochScopedNullifiersGlobalUnaffected(t *testing.T) {
	f := initFixture(t)

	// Domain 41 (rep challenges) is GLOBAL scoped in default genesis
	require.NoError(t, f.keeper.RecordNullifier(f.ctx, 41, 0, "global1", 10))
	require.NoError(t, f.keeper.RecordNullifier(f.ctx, 41, 0, "global2", 20))

	err := f.keeper.PruneEpochScopedNullifiers(f.ctx, 999999)
	require.NoError(t, err)

	// Global nullifiers should survive even aggressive pruning
	require.True(t, f.keeper.IsNullifierUsed(f.ctx, 41, 0, "global1"))
	require.True(t, f.keeper.IsNullifierUsed(f.ctx, 41, 0, "global2"))
}

func TestPendingNullifierIdempotent(t *testing.T) {
	f := initFixture(t)

	// Recording the same pending nullifier twice should be fine
	require.NoError(t, f.keeper.RecordPendingNullifier(f.ctx, "dup"))
	require.NoError(t, f.keeper.RecordPendingNullifier(f.ctx, "dup"))
	require.True(t, f.keeper.IsPendingNullifier(f.ctx, "dup"))

	// Deleting once should clear it
	require.NoError(t, f.keeper.DeletePendingNullifier(f.ctx, "dup"))
	require.False(t, f.keeper.IsPendingNullifier(f.ctx, "dup"))
}
