// Package crypto provides off-chain cryptographic utilities for the voting system.
//
// These utilities are used client-side to:
// - Generate and manage voter keys
// - Build Merkle trees of eligible voters
// - Generate Merkle proofs for individual voters
// - Compute nullifiers for votes
//
// IMPORTANT: The hash function used here (MiMC) MUST match the one used in the circuit.
// Any mismatch will cause proof verification to fail.
package crypto

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr/mimc"
)

// TreeDepth must match the circuit's TreeDepth constant
const TreeDepth = 20

// =====================================
// KEY MANAGEMENT
// =====================================

// VoterKeys contains a voter's key pair
type VoterKeys struct {
	SecretKey []byte // 32 bytes, must be kept private
	PublicKey []byte // 32 bytes, hash of secret key
}

// GenerateVoterKeys creates a new random voter key pair
func GenerateVoterKeys(entropy []byte) (*VoterKeys, error) {
	if len(entropy) < 32 {
		return nil, errors.New("entropy must be at least 32 bytes")
	}

	// Use the entropy as the secret key (in production, use proper key derivation)
	secretKey := make([]byte, 32)
	copy(secretKey, entropy[:32])

	// Compute public key = hash(secretKey)
	publicKey := HashToField(secretKey)

	return &VoterKeys{
		SecretKey: secretKey,
		PublicKey: publicKey,
	}, nil
}

// DerivePublicKey computes the public key from a secret key
func DerivePublicKey(secretKey []byte) []byte {
	return HashToField(secretKey)
}

// =====================================
// HASHING
// =====================================

// HashToField computes MiMC hash of inputs and returns a 32-byte field element
func HashToField(inputs ...[]byte) []byte {
	h := mimc.NewMiMC()
	for _, input := range inputs {
		// Pad each input to 32 bytes (field element size)
		padded := PadTo32(input)
		h.Write(padded)
	}
	return h.Sum(nil)
}

// HashTwoFields hashes two field elements together
func HashTwoFields(left, right []byte) []byte {
	h := mimc.NewMiMC()
	h.Write(PadTo32(left))
	h.Write(PadTo32(right))
	return h.Sum(nil)
}

// PadTo32 pads or truncates a byte slice to exactly 32 bytes
func PadTo32(data []byte) []byte {
	result := make([]byte, 32)
	if len(data) >= 32 {
		copy(result, data[:32])
	} else {
		// Left-pad with zeros for big-endian representation
		copy(result[32-len(data):], data)
	}
	return result
}

// =====================================
// NULLIFIER
// =====================================

// ComputeNullifier computes the nullifier for a vote
// nullifier = hash(secretKey, proposalID)
//
// The nullifier uniquely identifies a voter's vote on a specific proposal
// without revealing the voter's identity.
func ComputeNullifier(secretKey []byte, proposalID uint64) []byte {
	proposalBytes := Uint64ToBytes(proposalID)
	return HashToField(secretKey, proposalBytes)
}

// Uint64ToBytes converts a uint64 to a 32-byte big-endian representation
func Uint64ToBytes(n uint64) []byte {
	b := new(big.Int).SetUint64(n)
	return PadTo32(b.Bytes())
}

// =====================================
// LEAF COMPUTATION
// =====================================

// ComputeLeaf computes a Merkle tree leaf for a voter
// leaf = hash(publicKey, votingPower)
func ComputeLeaf(publicKey []byte, votingPower uint64) []byte {
	powerBytes := Uint64ToBytes(votingPower)
	return HashToField(publicKey, powerBytes)
}

// =====================================
// MERKLE TREE
// =====================================

// MerkleTree implements a binary Merkle tree using MiMC hash
type MerkleTree struct {
	depth    int
	leaves   [][]byte
	layers   [][][]byte // layers[0] = leaves, layers[depth] = [root]
	zeroHash [][]byte   // Pre-computed zero hashes for each level
}

// MerkleProof contains the data needed to prove membership in the tree
type MerkleProof struct {
	Root         []byte   // The Merkle root
	Leaf         []byte   // The leaf being proven
	LeafIndex    int      // Position of the leaf in the tree
	PathElements [][]byte // Sibling hashes along the path
	PathIndices  []uint64 // 0 = leaf is left child, 1 = leaf is right child
}

// NewMerkleTree creates a new Merkle tree with the given depth
func NewMerkleTree(depth int) *MerkleTree {
	if depth <= 0 || depth > 32 {
		panic("depth must be between 1 and 32")
	}

	mt := &MerkleTree{
		depth:    depth,
		leaves:   make([][]byte, 0),
		layers:   make([][][]byte, depth+1),
		zeroHash: make([][]byte, depth+1),
	}

	// Pre-compute zero hashes for padding empty leaves
	// zeroHash[0] = hash of empty leaf (all zeros)
	// zeroHash[i] = hash(zeroHash[i-1], zeroHash[i-1])
	mt.zeroHash[0] = make([]byte, 32)
	for i := 1; i <= depth; i++ {
		mt.zeroHash[i] = HashTwoFields(mt.zeroHash[i-1], mt.zeroHash[i-1])
	}

	return mt
}

// AddLeaf adds a leaf to the tree
// The leaf should be computed as hash(publicKey, votingPower)
func (mt *MerkleTree) AddLeaf(leaf []byte) error {
	maxLeaves := 1 << mt.depth
	if len(mt.leaves) >= maxLeaves {
		return fmt.Errorf("tree is full (max %d leaves)", maxLeaves)
	}
	mt.leaves = append(mt.leaves, PadTo32(leaf))
	return nil
}

// Build computes all internal nodes and the root
// Must be called after all leaves are added
func (mt *MerkleTree) Build() error {
	if len(mt.leaves) == 0 {
		return errors.New("no leaves in tree")
	}

	maxLeaves := 1 << mt.depth

	// Pad to power of 2 with zero hashes
	paddedLeaves := make([][]byte, maxLeaves)
	copy(paddedLeaves, mt.leaves)
	for i := len(mt.leaves); i < maxLeaves; i++ {
		paddedLeaves[i] = mt.zeroHash[0]
	}

	// Build layers from bottom up
	mt.layers[0] = paddedLeaves
	currentLayer := paddedLeaves

	for level := 1; level <= mt.depth; level++ {
		nextLayerSize := len(currentLayer) / 2
		nextLayer := make([][]byte, nextLayerSize)

		for i := 0; i < nextLayerSize; i++ {
			left := currentLayer[2*i]
			right := currentLayer[2*i+1]
			nextLayer[i] = HashTwoFields(left, right)
		}

		mt.layers[level] = nextLayer
		currentLayer = nextLayer
	}

	return nil
}

// Root returns the Merkle root
func (mt *MerkleTree) Root() []byte {
	if len(mt.layers) == 0 || len(mt.layers[mt.depth]) == 0 {
		return nil
	}
	return mt.layers[mt.depth][0]
}

// LeafCount returns the number of leaves in the tree
func (mt *MerkleTree) LeafCount() int {
	return len(mt.leaves)
}

// FindLeafIndex returns the index of a leaf in the tree, or -1 if not found.
func (mt *MerkleTree) FindLeafIndex(leaf []byte) int {
	padded := PadTo32(leaf)
	for i, l := range mt.leaves {
		if bytes.Equal(l, padded) {
			return i
		}
	}
	return -1
}

// GetProof generates a Merkle proof for the leaf at the given index
func (mt *MerkleTree) GetProof(leafIndex int) (*MerkleProof, error) {
	if leafIndex < 0 || leafIndex >= len(mt.leaves) {
		return nil, fmt.Errorf("leaf index %d out of range [0, %d)", leafIndex, len(mt.leaves))
	}

	if len(mt.layers[mt.depth]) == 0 {
		return nil, errors.New("tree not built - call Build() first")
	}

	pathElements := make([][]byte, mt.depth)
	pathIndices := make([]uint64, mt.depth)

	currentIndex := leafIndex

	for level := 0; level < mt.depth; level++ {
		// Determine if we're a left or right child
		isRightChild := currentIndex % 2
		pathIndices[level] = uint64(isRightChild)

		// Get sibling index
		var siblingIndex int
		if isRightChild == 0 {
			siblingIndex = currentIndex + 1
		} else {
			siblingIndex = currentIndex - 1
		}

		// Get sibling hash from this layer
		pathElements[level] = mt.layers[level][siblingIndex]

		// Move to parent index
		currentIndex = currentIndex / 2
	}

	return &MerkleProof{
		Root:         mt.Root(),
		Leaf:         mt.leaves[leafIndex],
		LeafIndex:    leafIndex,
		PathElements: pathElements,
		PathIndices:  pathIndices,
	}, nil
}

// Verify checks if a Merkle proof is valid
func (proof *MerkleProof) Verify() bool {
	if len(proof.PathElements) != len(proof.PathIndices) {
		return false
	}

	current := proof.Leaf

	for i := 0; i < len(proof.PathElements); i++ {
		sibling := proof.PathElements[i]

		var hash []byte
		if proof.PathIndices[i] == 0 {
			// We're the left child
			hash = HashTwoFields(current, sibling)
		} else {
			// We're the right child
			hash = HashTwoFields(sibling, current)
		}
		current = hash
	}

	// Compare computed root with expected root
	if len(current) != len(proof.Root) {
		return false
	}
	for i := range current {
		if current[i] != proof.Root[i] {
			return false
		}
	}
	return true
}

// =====================================
// FIELD ELEMENT CONVERSIONS
// =====================================

// BytesToFieldElement converts a byte slice to a gnark-compatible big.Int
func BytesToFieldElement(data []byte) *big.Int {
	return new(big.Int).SetBytes(PadTo32(data))
}

// Uint64ToFieldElement converts a uint64 to a gnark-compatible big.Int
func Uint64ToFieldElement(n uint64) *big.Int {
	return new(big.Int).SetUint64(n)
}

// FieldElementToBytes converts a field element to bytes
func FieldElementToBytes(elem *fr.Element) []byte {
	b := elem.Bytes()
	return b[:]
}
