package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeterministicShuffle(t *testing.T) {
	seed := []byte("test_seed_value_for_shuffle")

	t.Run("empty slice unchanged", func(t *testing.T) {
		result := deterministicShuffle([]int{}, seed)
		require.Empty(t, result)
	})

	t.Run("single element unchanged", func(t *testing.T) {
		result := deterministicShuffle([]int{42}, seed)
		require.Equal(t, []int{42}, result)
	})

	t.Run("deterministic with same seed", func(t *testing.T) {
		items := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
		result1 := deterministicShuffle(items, seed)
		result2 := deterministicShuffle(items, seed)
		require.Equal(t, result1, result2)
	})

	t.Run("different seed produces different order", func(t *testing.T) {
		items := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
		seed1 := []byte("seed_alpha")
		seed2 := []byte("seed_beta")
		result1 := deterministicShuffle(items, seed1)
		result2 := deterministicShuffle(items, seed2)
		// With 10 items and different seeds, the results should differ
		// (probability of identical shuffle with different seeds is ~1/10!)
		require.NotEqual(t, result1, result2)
	})

	t.Run("original slice not modified", func(t *testing.T) {
		original := []int{1, 2, 3, 4, 5}
		originalCopy := make([]int, len(original))
		copy(originalCopy, original)

		_ = deterministicShuffle(original, seed)
		require.Equal(t, originalCopy, original)
	})

	t.Run("all elements preserved", func(t *testing.T) {
		items := []int{10, 20, 30, 40, 50}
		result := deterministicShuffle(items, seed)
		require.Len(t, result, len(items))

		// Check all original elements are present
		elementSet := make(map[int]bool)
		for _, v := range result {
			elementSet[v] = true
		}
		for _, v := range items {
			require.True(t, elementSet[v], "element %d missing from shuffle result", v)
		}
	})

	t.Run("works with string type", func(t *testing.T) {
		items := []string{"alpha", "bravo", "charlie", "delta", "echo"}
		result1 := deterministicShuffle(items, seed)
		result2 := deterministicShuffle(items, seed)
		require.Equal(t, result1, result2)
		require.Len(t, result1, len(items))
	})
}

func TestMakeShuffleSeed(t *testing.T) {
	t.Run("deterministic", func(t *testing.T) {
		hash := []byte("block_hash_abc")
		seed1 := makeShuffleSeed(hash, 5)
		seed2 := makeShuffleSeed(hash, 5)
		require.Equal(t, seed1, seed2)
	})

	t.Run("different epoch produces different seed", func(t *testing.T) {
		hash := []byte("block_hash_abc")
		seed1 := makeShuffleSeed(hash, 5)
		seed2 := makeShuffleSeed(hash, 6)
		require.NotEqual(t, seed1, seed2)
	})

	t.Run("different hash produces different seed", func(t *testing.T) {
		seed1 := makeShuffleSeed([]byte("hash_one"), 5)
		seed2 := makeShuffleSeed([]byte("hash_two"), 5)
		require.NotEqual(t, seed1, seed2)
	})

	t.Run("output is 32 bytes (SHA-256)", func(t *testing.T) {
		seed := makeShuffleSeed([]byte("any_hash"), 1)
		require.Len(t, seed, 32)
	})

	t.Run("empty hash still produces seed", func(t *testing.T) {
		seed := makeShuffleSeed(nil, 0)
		require.Len(t, seed, 32)
	})
}
