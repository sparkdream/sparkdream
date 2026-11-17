package arweave

import (
	"crypto/sha256"
	"math/big"
)

// maxChunkSize is the chunk size of the data merkle tree.
const maxChunkSize = 256 * 1024

type merkleNode struct {
	id  [32]byte
	max int // the byte offset this node's data ends at
}

// note32 is an offset as a 32-byte big-endian number.
func note32(n int) []byte {
	out := make([]byte, 32)
	b := big.NewInt(int64(n)).Bytes()
	copy(out[32-len(b):], b)
	return out
}

func hashAll(parts ...[]byte) [32]byte {
	h := sha256.New()
	for _, p := range parts {
		h.Write(p)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// chunkEnds returns the end offset of each chunk of n bytes of data, cut the
// way a node cuts the data of a transaction posted inline (ar_tx:chunk_binary):
// 256 KiB chunks and whatever is left as the last one, however small. A
// final chunk is always appended, even an empty one (when the size is an
// exact multiple): the root of real transactions includes it.
//
// Clients that upload chunk by chunk split a tail under 32 KiB in two halves
// instead. That rule must not be used here: the node recomputes the root of
// inline data with the plain cut, and on a mismatch it keeps the transaction
// (and its reward) but drops the data.
func chunkEnds(n int) []int {
	var ends []int
	cursor, rest := 0, n
	for rest >= maxChunkSize {
		cursor += maxChunkSize
		rest -= maxChunkSize
		ends = append(ends, cursor)
	}
	return append(ends, cursor+rest)
}

// DataRoot is the merkle root over a transaction's data.
func DataRoot(data []byte) [32]byte {
	var layer []merkleNode
	start := 0
	for _, end := range chunkEnds(len(data)) {
		dataHash := sha256.Sum256(data[start:end])
		a := sha256.Sum256(dataHash[:])
		b := sha256.Sum256(note32(end))
		layer = append(layer, merkleNode{id: hashAll(a[:], b[:]), max: end})
		start = end
	}
	for len(layer) > 1 {
		var next []merkleNode
		for i := 0; i < len(layer); i += 2 {
			if i+1 == len(layer) {
				next = append(next, layer[i])
				break
			}
			l, r := layer[i], layer[i+1]
			a := sha256.Sum256(l.id[:])
			b := sha256.Sum256(r.id[:])
			c := sha256.Sum256(note32(l.max))
			next = append(next, merkleNode{id: hashAll(a[:], b[:], c[:]), max: r.max})
		}
		layer = next
	}
	return layer[0].id
}
