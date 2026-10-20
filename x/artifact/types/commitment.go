package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"regexp"

	errorsmod "cosmossdk.io/errors"
)

// Commit-reveal primitives (docs/x-artifact-spec.md §7.15).
//
// Blind mints commit every token's metadata in one Merkle root at class
// creation. A leaf binds the class, the token id, a per-token salt and the
// metadata hash, so an issuer cannot reassign revealed metadata between
// tokens and a collector cannot brute-force the salt-less metadata space.
// Nodes hash the sorted pair of their children, so a proof is just the
// sibling hashes from the leaf up, without position bits.
//
// Generative classes commit SHA-256(seed) and reveal the seed once; a
// token's seed is derived from it with the class and token id.

const (
	// MaxRevealProofDepth bounds a MsgReveal proof (2^32 leaves).
	MaxRevealProofDepth = 32
	// MaxRevealSecretBytes bounds the salt and seed secrets.
	MaxRevealSecretBytes = 64

	leafDomain = 0x00
	nodeDomain = 0x01
	seedDomain = "sparkdream/artifact/seed/v1"
)

var hexBytesPattern = regexp.MustCompile(`^([0-9a-f]{2})+$`)

// DecodeHexSecret parses a lowercase-hex salt or seed of 1..MaxRevealSecretBytes.
func DecodeHexSecret(field, s string) ([]byte, error) {
	if s == "" || !hexBytesPattern.MatchString(s) {
		return nil, errorsmod.Wrapf(ErrInvalidReveal, "%s must be lowercase hex bytes", field)
	}
	bz, err := hex.DecodeString(s)
	if err != nil {
		return nil, errorsmod.Wrapf(ErrInvalidReveal, "%s: %v", field, err)
	}
	if len(bz) > MaxRevealSecretBytes {
		return nil, errorsmod.Wrapf(ErrInvalidReveal, "%s exceeds %d bytes", field, MaxRevealSecretBytes)
	}
	return bz, nil
}

// DecodeHexHash parses a lowercase-hex SHA-256.
func DecodeHexHash(field, s string) ([]byte, error) {
	if !hexHashPattern.MatchString(s) {
		return nil, errorsmod.Wrapf(ErrInvalidReveal, "%s must be 64 lowercase hex characters", field)
	}
	return hex.DecodeString(s)
}

// RevealLeaf is the Merkle leaf committing token (classID, tokenID) to
// metadata under salt:
//
//	SHA-256(0x00 || be64(class_id) || be64(token_id) || salt || SHA-256(metadata))
func RevealLeaf(classID, tokenID uint64, salt []byte, m TokenMetadata) []byte {
	mh, _ := hex.DecodeString(MetadataHash(m))
	h := sha256.New()
	h.Write([]byte{leafDomain})
	var be [8]byte
	binary.BigEndian.PutUint64(be[:], classID)
	h.Write(be[:])
	binary.BigEndian.PutUint64(be[:], tokenID)
	h.Write(be[:])
	h.Write(salt)
	h.Write(mh)
	return h.Sum(nil)
}

// RevealNode hashes a sorted pair of child hashes:
// SHA-256(0x01 || min(a,b) || max(a,b)).
func RevealNode(a, b []byte) []byte {
	if bytes.Compare(a, b) > 0 {
		a, b = b, a
	}
	h := sha256.New()
	h.Write([]byte{nodeDomain})
	h.Write(a)
	h.Write(b)
	return h.Sum(nil)
}

// VerifyRevealProof folds leaf up through proof and compares to root.
func VerifyRevealProof(leaf []byte, proof [][]byte, root []byte) bool {
	cur := leaf
	for _, sib := range proof {
		cur = RevealNode(cur, sib)
	}
	return bytes.Equal(cur, root)
}

// BuildRevealTree computes the root and per-leaf proofs over leaves (in
// order). Odd levels carry the last node up unchanged. Clients and tests use
// it to produce commitments; the chain only verifies.
func BuildRevealTree(leaves [][]byte) (root []byte, proofs [][][]byte) {
	n := len(leaves)
	if n == 0 {
		return nil, nil
	}
	proofs = make([][][]byte, n)
	// idx[i] = position of leaf i's ancestor in the current level.
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	level := make([][]byte, n)
	copy(level, leaves)
	for len(level) > 1 {
		next := make([][]byte, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			if i+1 == len(level) {
				next = append(next, level[i])
				continue
			}
			next = append(next, RevealNode(level[i], level[i+1]))
		}
		for leaf, pos := range idx {
			if pos^1 < len(level) {
				proofs[leaf] = append(proofs[leaf], level[pos^1])
			}
			idx[leaf] = pos / 2
		}
		level = next
	}
	return level[0], proofs
}

// SeedCommitment is the hex SHA-256 a generative class commits to.
func SeedCommitment(seed []byte) string {
	sum := sha256.Sum256(seed)
	return hex.EncodeToString(sum[:])
}

// TokenSeed derives a token's seed from the revealed class seed.
func TokenSeed(seed []byte, classID, tokenID uint64) string {
	h := sha256.New()
	h.Write([]byte(seedDomain))
	h.Write(seed)
	var be [8]byte
	binary.BigEndian.PutUint64(be[:], classID)
	h.Write(be[:])
	binary.BigEndian.PutUint64(be[:], tokenID)
	h.Write(be[:])
	return hex.EncodeToString(h.Sum(nil))
}

// HexHashes encodes hashes as lowercase hex (proof wire form).
func HexHashes(hs [][]byte) []string {
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = hex.EncodeToString(h)
	}
	return out
}
