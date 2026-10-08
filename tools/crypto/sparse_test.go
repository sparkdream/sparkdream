package crypto

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
)

func TestSparseMerkleProofMatchesDenseTree(t *testing.T) {
	const depth = 6
	// Occupied slots with a gap (index 2 empty), as when a member left.
	leaves := map[uint64][]byte{
		0: ComputeLeaf(DerivePublicKey([]byte{1}), 1),
		1: ComputeLeaf(DerivePublicKey([]byte{2}), 2),
		3: ComputeLeaf(DerivePublicKey([]byte{3}), 3),
		9: ComputeLeaf(DerivePublicKey([]byte{4}), 4),
	}

	dense := NewMerkleTree(depth)
	for i := uint64(0); i <= 9; i++ {
		leaf, ok := leaves[i]
		if !ok {
			leaf = make([]byte, 32)
		}
		if err := dense.AddLeaf(leaf); err != nil {
			t.Fatal(err)
		}
	}
	if err := dense.Build(); err != nil {
		t.Fatal(err)
	}

	for idx := range leaves {
		got, err := SparseMerkleProof(leaves, depth, idx)
		if err != nil {
			t.Fatal(err)
		}
		want, err := dense.GetProof(int(idx))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Root, want.Root) {
			t.Fatalf("leaf %d: root mismatch", idx)
		}
		for d := range want.PathElements {
			if !bytes.Equal(got.PathElements[d], want.PathElements[d]) || got.PathIndices[d] != want.PathIndices[d] {
				t.Fatalf("leaf %d: path mismatch at level %d", idx, d)
			}
		}
		if !got.Verify() {
			t.Fatalf("leaf %d: sparse proof does not verify", idx)
		}
	}

	if _, err := SparseMerkleProof(leaves, depth, 2); err == nil {
		t.Fatal("expected an error for an empty slot")
	}
}

func TestDeriveSecretKeyFromSignature(t *testing.T) {
	sig := bytes.Repeat([]byte{0xab}, 64)
	a, err := DeriveSecretKeyFromSignature(sig)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := DeriveSecretKeyFromSignature(sig)
	if !bytes.Equal(a, b) {
		t.Fatal("derivation must be deterministic")
	}
	if !IsCanonicalFieldElement(a) {
		t.Fatal("derived key must be a canonical field element")
	}
	other, _ := DeriveSecretKeyFromSignature(append(sig[:63:63], 0xac))
	if bytes.Equal(a, other) {
		t.Fatal("different signatures must derive different keys")
	}
	if _, err := DeriveSecretKeyFromSignature(nil); err == nil {
		t.Fatal("expected an error for an empty signature")
	}
}

func TestIsCanonicalFieldElement(t *testing.T) {
	mod := fr.Modulus()
	below := new(big.Int).Sub(mod, big.NewInt(1)).FillBytes(make([]byte, 32))
	if !IsCanonicalFieldElement(below) {
		t.Fatal("modulus-1 must be canonical")
	}
	if IsCanonicalFieldElement(mod.FillBytes(make([]byte, 32))) {
		t.Fatal("the modulus itself must not be canonical")
	}
	if IsCanonicalFieldElement(bytes.Repeat([]byte{0xff}, 32)) {
		t.Fatal("2^256-1 must not be canonical")
	}
}

func TestShieldMessageHash(t *testing.T) {
	a := ShieldMessageHash("/sparkdream.blog.v1.MsgCreatePost", []byte("phoenix"))
	if !bytes.Equal(a, ShieldMessageHash("/sparkdream.blog.v1.MsgCreatePost", []byte("phoenix"))) {
		t.Fatal("hash must be deterministic")
	}
	if !IsCanonicalFieldElement(a) {
		t.Fatal("hash must be a canonical field element")
	}
	if bytes.Equal(a, ShieldMessageHash("/sparkdream.blog.v1.MsgCreatePost", []byte("aurora"))) {
		t.Fatal("different values must hash differently")
	}
	// The type URL is length-prefixed, so moving bytes between type URL and
	// value changes the hash.
	if bytes.Equal(ShieldMessageHash("ab", []byte("c")), ShieldMessageHash("a", []byte("bc"))) {
		t.Fatal("type URL / value boundary must be unambiguous")
	}
}
