package crypto

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

// SparseMerkleProof computes the root of a depth-`depth` MiMC tree holding only
// `leaves` (index → leaf hash; every other slot is the zero leaf) and the
// authentication path of leafIndex. It hashes O(len(leaves)·depth) nodes, where
// MerkleTree.Build hashes all 2^depth slots. Produces the same root as x/rep's
// trust tree and MerkleTree for the same leaves.
func SparseMerkleProof(leaves map[uint64][]byte, depth int, leafIndex uint64) (*MerkleProof, error) {
	if depth <= 0 || depth > 32 {
		return nil, fmt.Errorf("depth must be between 1 and 32, got %d", depth)
	}
	if leafIndex >= uint64(1)<<depth {
		return nil, fmt.Errorf("leaf index %d outside a depth-%d tree", leafIndex, depth)
	}
	leaf, ok := leaves[leafIndex]
	if !ok {
		return nil, fmt.Errorf("no leaf at index %d", leafIndex)
	}

	zero := make([][]byte, depth+1)
	zero[0] = make([]byte, 32)
	for i := 1; i <= depth; i++ {
		zero[i] = HashTwoFields(zero[i-1], zero[i-1])
	}

	level := make(map[uint64][]byte, len(leaves))
	for idx, h := range leaves {
		if idx >= uint64(1)<<depth {
			return nil, fmt.Errorf("leaf index %d outside a depth-%d tree", idx, depth)
		}
		level[idx] = PadTo32(h)
	}
	node := func(lvl map[uint64][]byte, d int, idx uint64) []byte {
		if h, ok := lvl[idx]; ok {
			return h
		}
		return zero[d]
	}

	path := make([][]byte, depth)
	indices := make([]uint64, depth)
	cur := leafIndex
	for d := 0; d < depth; d++ {
		indices[d] = cur & 1
		path[d] = node(level, d, cur^1)

		parents := make(map[uint64][]byte, (len(level)+1)/2)
		for idx := range level {
			p := idx >> 1
			if _, done := parents[p]; done {
				continue
			}
			parents[p] = HashTwoFields(node(level, d, p<<1), node(level, d, p<<1|1))
		}
		level = parents
		cur >>= 1
	}

	return &MerkleProof{
		Root:         node(level, depth, 0),
		Leaf:         PadTo32(leaf),
		LeafIndex:    int(leafIndex),
		PathElements: path,
		PathIndices:  indices,
	}, nil
}

// secretKeyDomain separates shield secret-key derivation from any other use of
// the same signature.
const secretKeyDomain = "sparkdream/shield/secret-key/v1"

// DeriveSecretKeyFromSignature turns a wallet signature over a fixed message
// into a shield secret key, so the key can be recovered on any device holding
// the same wallet. The result is a canonical BN254 scalar: candidates are
// sha256(domain || counter || signature) with the top two bits cleared, taking
// the first one below the field modulus.
func DeriveSecretKeyFromSignature(signature []byte) ([]byte, error) {
	if len(signature) == 0 {
		return nil, errors.New("empty signature")
	}
	var counter [4]byte
	for i := uint32(0); i < 256; i++ {
		binary.BigEndian.PutUint32(counter[:], i)
		h := sha256.Sum256(bytes.Join([][]byte{[]byte(secretKeyDomain), counter[:], signature}, nil))
		h[0] &= 0x3f
		if IsCanonicalFieldElement(h[:]) {
			return h[:], nil
		}
	}
	return nil, errors.New("no canonical secret key derived")
}

// shieldMessageDomain separates the message-binding hash from other hashes.
const shieldMessageDomain = "sparkdream/shield/message/v1"

// shieldOwnedMessageDomain separates ownership-mode message hashes, which also
// bind the owner's sequence, from ordinary ones.
const shieldOwnedMessageDomain = "sparkdream/shield/owned-message/v1"

// ShieldMessageHash is the field element a shield proof binds its inner
// message to: sha256(domain || len(typeURL) || typeURL || value) with the top
// three bits cleared, which keeps it below the BN254 scalar modulus. value is
// the inner Any's bytes exactly as submitted, so prover and chain never
// re-encode the message.
func ShieldMessageHash(typeURL string, value []byte) []byte {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(typeURL)))
	h := sha256.Sum256(bytes.Join([][]byte{[]byte(shieldMessageDomain), n[:], []byte(typeURL), value}, nil))
	h[0] &= 0x1f
	return h[:]
}

// ShieldOwnedMessageHash is the message hash for an ownership-mode exec (one
// that proves it is the owner of anonymously created content): the inner Any
// plus the owner's current sequence, sha256(domain || len(typeURL) || typeURL
// || value || be64(sequence)) with the top three bits cleared. Ownership proofs
// reuse the same nullifier on purpose, so the sequence is what stops a replayed
// or rewrapped exec: once it advances, an old proof no longer verifies.
func ShieldOwnedMessageHash(typeURL string, value []byte, sequence uint64) []byte {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(typeURL)))
	var seq [8]byte
	binary.BigEndian.PutUint64(seq[:], sequence)
	h := sha256.Sum256(bytes.Join([][]byte{[]byte(shieldOwnedMessageDomain), n[:], []byte(typeURL), value, seq[:]}, nil))
	h[0] &= 0x1f
	return h[:]
}
