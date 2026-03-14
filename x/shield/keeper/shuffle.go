package keeper

import (
	"crypto/sha256"
	"encoding/binary"
)

// deterministicShuffle performs a Fisher-Yates shuffle using a deterministic seed.
// The seed is derived from the block hash and epoch to ensure unpredictability
// at submission time but determinism at execution time.
func deterministicShuffle[T any](items []T, seed []byte) []T {
	n := len(items)
	if n <= 1 {
		return items
	}

	// Create a copy to avoid mutating the original
	result := make([]T, n)
	copy(result, items)

	// Fisher-Yates shuffle using successive hashes as random source
	h := seed
	for i := n - 1; i > 0; i-- {
		// Generate next random value from hash chain
		hash := sha256.Sum256(h)
		h = hash[:]

		// Convert first 8 bytes to uint64 for index selection
		j := int(binary.BigEndian.Uint64(h[:8]) % uint64(i+1))

		result[i], result[j] = result[j], result[i]
	}

	return result
}

// makeShuffleSeed creates the seed for batch shuffling from block hash and epoch.
func makeShuffleSeed(lastBlockHash []byte, epoch uint64) []byte {
	h := sha256.New()
	h.Write(lastBlockHash)
	epochBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(epochBytes, epoch)
	h.Write(epochBytes)
	return h.Sum(nil)
}
