package types_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/types"
)

func TestRevealTreeRoundTrip(t *testing.T) {
	for _, n := range []int{1, 2, 3, 5, 8, 17} {
		leaves := make([][]byte, n)
		for i := range leaves {
			leaves[i] = types.RevealLeaf(7, uint64(i+1), []byte{byte(i)}, types.TokenMetadata{Name: fmt.Sprint("t", i)})
		}
		root, proofs := types.BuildRevealTree(leaves)
		require.Len(t, root, 32)
		require.Len(t, proofs, n)
		for i, leaf := range leaves {
			require.True(t, types.VerifyRevealProof(leaf, proofs[i], root), "n=%d leaf %d", n, i)
			// A proof never verifies another leaf, nor a tampered leaf.
			other := types.RevealLeaf(7, uint64(i+1), []byte{byte(i)}, types.TokenMetadata{Name: "x"})
			require.False(t, types.VerifyRevealProof(other, proofs[i], root))
			if n > 1 {
				require.False(t, types.VerifyRevealProof(leaf, proofs[(i+1)%n], root), "n=%d leaf %d", n, i)
			}
		}
	}
	root, proofs := types.BuildRevealTree(nil)
	require.Nil(t, root)
	require.Nil(t, proofs)
}

func TestRevealLeafBindsEveryInput(t *testing.T) {
	m := types.TokenMetadata{Name: "aurora"}
	base := types.RevealLeaf(1, 1, []byte("salt"), m)
	require.NotEqual(t, base, types.RevealLeaf(2, 1, []byte("salt"), m), "class id")
	require.NotEqual(t, base, types.RevealLeaf(1, 2, []byte("salt"), m), "token id")
	require.NotEqual(t, base, types.RevealLeaf(1, 1, []byte("salt2"), m), "salt")
	require.NotEqual(t, base, types.RevealLeaf(1, 1, []byte("salt"), types.TokenMetadata{Name: "zenith"}), "metadata")
	// Node hashing is commutative, so proofs need no position bits.
	a, b := sha256.Sum256([]byte("a")), sha256.Sum256([]byte("b"))
	require.Equal(t, types.RevealNode(a[:], b[:]), types.RevealNode(b[:], a[:]))
	// Leaves and nodes live in separate domains.
	require.NotEqual(t, types.RevealNode(a[:], b[:]), sha256.Sum256(append(a[:], b[:]...)))
}

func TestSeedDerivation(t *testing.T) {
	seed := []byte("phoenix-secret")
	commitment := types.SeedCommitment(seed)
	sum := sha256.Sum256(seed)
	require.Equal(t, hex.EncodeToString(sum[:]), commitment)
	s1 := types.TokenSeed(seed, 1, 1)
	require.Len(t, s1, 64)
	require.NotEqual(t, s1, types.TokenSeed(seed, 1, 2))
	require.NotEqual(t, s1, types.TokenSeed(seed, 2, 1))
	require.NotEqual(t, s1, types.TokenSeed([]byte("other"), 1, 1))
}

func TestDecodeHexSecret(t *testing.T) {
	bz, err := types.DecodeHexSecret("salt", "00ff")
	require.NoError(t, err)
	require.Equal(t, []byte{0, 0xff}, bz)
	for _, bad := range []string{"", "0", "ABCD", "zz", "0g"} {
		_, err := types.DecodeHexSecret("salt", bad)
		require.ErrorIs(t, err, types.ErrInvalidReveal, bad)
	}
	long := make([]byte, types.MaxRevealSecretBytes+1)
	_, err = types.DecodeHexSecret("salt", hex.EncodeToString(long))
	require.ErrorIs(t, err, types.ErrInvalidReveal)
	_, err = types.DecodeHexHash("commitment", "abcd")
	require.ErrorIs(t, err, types.ErrInvalidReveal)
	_, err = types.DecodeHexHash("commitment", types.SeedCommitment([]byte("x")))
	require.NoError(t, err)
}
