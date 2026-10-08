package circuit

import (
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"

	zkcrypto "sparkdream/tools/crypto"
)

// TestShieldCircuit_MessageHashIsBound proves with the real circuit and checks
// that the proof verifies only for the message hash it was made for. A public
// input left out of every constraint would verify with any value.
func TestShieldCircuit_MessageHashIsBound(t *testing.T) {
	if testing.Short() {
		t.Skip("full Groth16 setup")
	}
	var c ShieldCircuit
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &c)
	if err != nil {
		t.Fatal(err)
	}
	pk, vk, err := groth16.Setup(ccs)
	if err != nil {
		t.Fatal(err)
	}

	sk := zkcrypto.PadTo32([]byte{7})
	const trust, scope, epoch = 2, 3, 4
	path, err := zkcrypto.SparseMerkleProof(map[uint64][]byte{
		0: zkcrypto.ComputeLeaf(zkcrypto.DerivePublicKey(sk), trust),
	}, TreeDepth, 0)
	if err != nil {
		t.Fatal(err)
	}
	msgHash := zkcrypto.ShieldMessageHash("/sparkdream.blog.v1.MsgCreatePost", []byte("original"))
	public := func(hash []byte) *ShieldCircuit {
		return &ShieldCircuit{
			MerkleRoot:         new(big.Int).SetBytes(path.Root),
			Nullifier:          new(big.Int).SetBytes(zkcrypto.ComputeNullifier(sk, scope)),
			RateLimitNullifier: new(big.Int).SetBytes(zkcrypto.ComputeRateLimitNullifier(sk, epoch)),
			MinTrustLevel:      1,
			Scope:              scope,
			RateLimitEpoch:     epoch,
			MessageHash:        new(big.Int).SetBytes(hash),
		}
	}

	full := public(msgHash)
	full.SecretKey = new(big.Int).SetBytes(sk)
	full.TrustLevel = trust
	for i := 0; i < TreeDepth; i++ {
		full.PathElements[i] = new(big.Int).SetBytes(path.PathElements[i])
		full.PathIndices[i] = path.PathIndices[i]
	}
	w, err := frontend.NewWitness(full, ecc.BN254.ScalarField())
	if err != nil {
		t.Fatal(err)
	}
	proof, err := groth16.Prove(ccs, pk, w)
	if err != nil {
		t.Fatal(err)
	}

	verify := func(hash []byte) error {
		pw, err := frontend.NewWitness(public(hash), ecc.BN254.ScalarField(), frontend.PublicOnly())
		if err != nil {
			t.Fatal(err)
		}
		return groth16.Verify(proof, vk, pw)
	}
	if err := verify(msgHash); err != nil {
		t.Fatalf("proof must verify for its own message: %v", err)
	}
	tampered := zkcrypto.ShieldMessageHash("/sparkdream.blog.v1.MsgCreatePost", []byte("swapped"))
	if err := verify(tampered); err == nil {
		t.Fatal("proof must not verify for a different message")
	}
}

// TestSmallShieldCircuit_MessageHashIsBound pins the same property on the
// depth-4 test circuit so it runs under -short: a proof made for one message
// hash verifies for that hash and not for another.
func TestSmallShieldCircuit_MessageHashIsBound(t *testing.T) {
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &smallShieldCircuit{})
	if err != nil {
		t.Fatal(err)
	}
	pk, vk, err := groth16.Setup(ccs)
	if err != nil {
		t.Fatal(err)
	}

	sk := zkcrypto.PadTo32([]byte("test-secret-key-message-binding"))
	const trust = 2
	tree := buildSmallTrustTree([][]byte{zkcrypto.DerivePublicKey(sk)}, []uint64{trust})
	msgHash := zkcrypto.ShieldMessageHash("/sparkdream.blog.v1.MsgCreatePost", []byte("original"))
	full := makeAssignment(sk, trust, 1, 3, 4, tree, 0)
	full.MessageHash = new(big.Int).SetBytes(msgHash)

	w, err := frontend.NewWitness(full, ecc.BN254.ScalarField())
	if err != nil {
		t.Fatal(err)
	}
	proof, err := groth16.Prove(ccs, pk, w)
	if err != nil {
		t.Fatal(err)
	}

	verify := func(hash []byte) error {
		public := *full
		public.MessageHash = new(big.Int).SetBytes(hash)
		pw, err := frontend.NewWitness(&public, ecc.BN254.ScalarField(), frontend.PublicOnly())
		if err != nil {
			t.Fatal(err)
		}
		return groth16.Verify(proof, vk, pw)
	}
	if err := verify(msgHash); err != nil {
		t.Fatalf("proof must verify for its own message: %v", err)
	}
	tampered := zkcrypto.ShieldMessageHash("/sparkdream.blog.v1.MsgCreatePost", []byte("swapped"))
	if err := verify(tampered); err == nil {
		t.Fatal("proof must not verify for a different message")
	}
}
