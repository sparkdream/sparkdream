package circuit

import (
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/hash/mimc"
	"github.com/consensys/gnark/test"

	zkcrypto "sparkdream/zkprivatevoting/crypto"
)

// smallAnonActionCircuit is a test-sized circuit with depth 4 (supports 16 members).
type smallAnonActionCircuit struct {
	MerkleRoot    frontend.Variable `gnark:",public"`
	Nullifier     frontend.Variable `gnark:",public"`
	MinTrustLevel frontend.Variable `gnark:",public"`
	Scope         frontend.Variable `gnark:",public"`

	SecretKey    frontend.Variable
	TrustLevel   frontend.Variable
	PathElements [4]frontend.Variable
	PathIndices  [4]frontend.Variable
}

func (c *smallAnonActionCircuit) Define(api frontend.API) error {
	hFunc, err := mimc.NewMiMC(api)
	if err != nil {
		return err
	}

	// Constraint 1: publicKey = hash(secretKey)
	hFunc.Reset()
	hFunc.Write(c.SecretKey)
	publicKey := hFunc.Sum()

	// Constraint 2: leaf = hash(publicKey, trustLevel)
	hFunc.Reset()
	hFunc.Write(publicKey)
	hFunc.Write(c.TrustLevel)
	leaf := hFunc.Sum()

	// Constraint 3: Merkle proof
	current := leaf
	for i := 0; i < 4; i++ {
		sibling := c.PathElements[i]

		hFunc.Reset()
		hFunc.Write(current)
		hFunc.Write(sibling)
		hashAsLeft := hFunc.Sum()

		hFunc.Reset()
		hFunc.Write(sibling)
		hFunc.Write(current)
		hashAsRight := hFunc.Sum()

		current = api.Select(c.PathIndices[i], hashAsRight, hashAsLeft)
	}
	api.AssertIsEqual(current, c.MerkleRoot)

	// Constraint 4: trustLevel >= minTrustLevel
	api.AssertIsLessOrEqual(c.MinTrustLevel, c.TrustLevel)

	// Constraint 5: nullifier = hash(secretKey, scope)
	hFunc.Reset()
	hFunc.Write(c.SecretKey)
	hFunc.Write(c.Scope)
	expectedNullifier := hFunc.Sum()
	api.AssertIsEqual(expectedNullifier, c.Nullifier)

	// Constraint 6: path indices are binary
	for i := 0; i < 4; i++ {
		vMinus1 := api.Sub(c.PathIndices[i], 1)
		product := api.Mul(c.PathIndices[i], vMinus1)
		api.AssertIsEqual(product, 0)
	}

	return nil
}

// buildSmallTrustTree builds a depth-4 MiMC Merkle tree with trust-level-encoded leaves.
func buildSmallTrustTree(pubKeys [][]byte, trustLevels []uint64) *zkcrypto.MerkleTree {
	tree := zkcrypto.NewMerkleTree(4)
	for i, pk := range pubKeys {
		// leaf = hash(publicKey, trustLevel) — same as ComputeLeaf but with trust level
		leaf := zkcrypto.ComputeLeaf(pk, trustLevels[i])
		tree.AddLeaf(leaf) //nolint:errcheck
	}
	if len(pubKeys) > 0 {
		tree.Build() //nolint:errcheck
	}
	return tree
}

func TestAnonActionCircuit_ValidProof(t *testing.T) {
	// Create a member with trust level 2 (ESTABLISHED)
	secretKey := zkcrypto.PadTo32([]byte("test-secret-key-for-anon-action"))
	publicKey := zkcrypto.DerivePublicKey(secretKey)
	trustLevel := uint64(2) // ESTABLISHED
	scope := uint64(42)     // e.g., epoch number

	// Build a small tree with 3 members
	otherPK1 := zkcrypto.DerivePublicKey(zkcrypto.PadTo32([]byte("other-member-1")))
	otherPK2 := zkcrypto.DerivePublicKey(zkcrypto.PadTo32([]byte("other-member-2")))

	pubKeys := [][]byte{publicKey, otherPK1, otherPK2}
	trustLevels := []uint64{trustLevel, 1, 3}

	tree := buildSmallTrustTree(pubKeys, trustLevels)

	// Our leaf is at index 0
	leaf := zkcrypto.ComputeLeaf(publicKey, trustLevel)
	leafIndex := tree.FindLeafIndex(leaf)
	if leafIndex < 0 {
		t.Fatal("leaf not found in tree")
	}

	proof, err := tree.GetProof(leafIndex)
	if err != nil {
		t.Fatal(err)
	}

	// Compute nullifier = hash(secretKey, scope)
	nullifier := zkcrypto.HashToField(secretKey, zkcrypto.Uint64ToBytes(scope))

	// Build witness
	assignment := &smallAnonActionCircuit{
		MerkleRoot:    new(big.Int).SetBytes(zkcrypto.PadTo32(tree.Root())),
		Nullifier:     new(big.Int).SetBytes(zkcrypto.PadTo32(nullifier)),
		MinTrustLevel: uint64(1), // Require at least PROVISIONAL (1)
		Scope:         scope,
		SecretKey:     new(big.Int).SetBytes(zkcrypto.PadTo32(secretKey)),
		TrustLevel:    trustLevel,
	}

	for i := 0; i < 4; i++ {
		assignment.PathElements[i] = new(big.Int).SetBytes(zkcrypto.PadTo32(proof.PathElements[i]))
		assignment.PathIndices[i] = proof.PathIndices[i]
	}

	assert := test.NewAssert(t)
	assert.ProverSucceeded(&smallAnonActionCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func TestAnonActionCircuit_InsufficientTrustLevel(t *testing.T) {
	// Create a member with trust level 0 (NEW) trying to prove level >= 1
	secretKey := zkcrypto.PadTo32([]byte("test-secret-key-insufficient"))
	publicKey := zkcrypto.DerivePublicKey(secretKey)
	trustLevel := uint64(0) // NEW
	scope := uint64(42)

	tree := buildSmallTrustTree([][]byte{publicKey}, []uint64{trustLevel})

	leaf := zkcrypto.ComputeLeaf(publicKey, trustLevel)
	proof, err := tree.GetProof(tree.FindLeafIndex(leaf))
	if err != nil {
		t.Fatal(err)
	}

	nullifier := zkcrypto.HashToField(secretKey, zkcrypto.Uint64ToBytes(scope))

	assignment := &smallAnonActionCircuit{
		MerkleRoot:    new(big.Int).SetBytes(zkcrypto.PadTo32(tree.Root())),
		Nullifier:     new(big.Int).SetBytes(zkcrypto.PadTo32(nullifier)),
		MinTrustLevel: uint64(1), // Require PROVISIONAL but member is NEW
		Scope:         scope,
		SecretKey:     new(big.Int).SetBytes(zkcrypto.PadTo32(secretKey)),
		TrustLevel:    trustLevel,
	}

	for i := 0; i < 4; i++ {
		assignment.PathElements[i] = new(big.Int).SetBytes(zkcrypto.PadTo32(proof.PathElements[i]))
		assignment.PathIndices[i] = proof.PathIndices[i]
	}

	assert := test.NewAssert(t)
	assert.ProverFailed(&smallAnonActionCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func TestAnonActionCircuit_WrongScope(t *testing.T) {
	// Prove with correct scope but submit nullifier from wrong scope
	secretKey := zkcrypto.PadTo32([]byte("test-secret-key-wrong-scope"))
	publicKey := zkcrypto.DerivePublicKey(secretKey)
	trustLevel := uint64(2)
	correctScope := uint64(42)
	wrongScope := uint64(99)

	tree := buildSmallTrustTree([][]byte{publicKey}, []uint64{trustLevel})

	leaf := zkcrypto.ComputeLeaf(publicKey, trustLevel)
	proof, err := tree.GetProof(tree.FindLeafIndex(leaf))
	if err != nil {
		t.Fatal(err)
	}

	// Compute nullifier with wrong scope
	nullifier := zkcrypto.HashToField(secretKey, zkcrypto.Uint64ToBytes(wrongScope))

	assignment := &smallAnonActionCircuit{
		MerkleRoot:    new(big.Int).SetBytes(zkcrypto.PadTo32(tree.Root())),
		Nullifier:     new(big.Int).SetBytes(zkcrypto.PadTo32(nullifier)),
		MinTrustLevel: uint64(1),
		Scope:         correctScope, // Scope doesn't match the nullifier
		SecretKey:     new(big.Int).SetBytes(zkcrypto.PadTo32(secretKey)),
		TrustLevel:    trustLevel,
	}

	for i := 0; i < 4; i++ {
		assignment.PathElements[i] = new(big.Int).SetBytes(zkcrypto.PadTo32(proof.PathElements[i]))
		assignment.PathIndices[i] = proof.PathIndices[i]
	}

	assert := test.NewAssert(t)
	assert.ProverFailed(&smallAnonActionCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func TestAnonActionCircuit_WrongMerkleRoot(t *testing.T) {
	// Build proof against one tree, but verify against a different root
	secretKey := zkcrypto.PadTo32([]byte("test-secret-key-wrong-root"))
	publicKey := zkcrypto.DerivePublicKey(secretKey)
	trustLevel := uint64(2)
	scope := uint64(42)

	tree := buildSmallTrustTree([][]byte{publicKey}, []uint64{trustLevel})

	leaf := zkcrypto.ComputeLeaf(publicKey, trustLevel)
	proof, err := tree.GetProof(tree.FindLeafIndex(leaf))
	if err != nil {
		t.Fatal(err)
	}

	nullifier := zkcrypto.HashToField(secretKey, zkcrypto.Uint64ToBytes(scope))

	// Use a fake merkle root
	fakeRoot := zkcrypto.HashToField([]byte("fake-root"))

	assignment := &smallAnonActionCircuit{
		MerkleRoot:    new(big.Int).SetBytes(zkcrypto.PadTo32(fakeRoot)),
		Nullifier:     new(big.Int).SetBytes(zkcrypto.PadTo32(nullifier)),
		MinTrustLevel: uint64(1),
		Scope:         scope,
		SecretKey:     new(big.Int).SetBytes(zkcrypto.PadTo32(secretKey)),
		TrustLevel:    trustLevel,
	}

	for i := 0; i < 4; i++ {
		assignment.PathElements[i] = new(big.Int).SetBytes(zkcrypto.PadTo32(proof.PathElements[i]))
		assignment.PathIndices[i] = proof.PathIndices[i]
	}

	assert := test.NewAssert(t)
	assert.ProverFailed(&smallAnonActionCircuit{}, assignment, test.WithCurves(ecc.BN254))
}
