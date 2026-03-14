package circuit

import (
	"math"
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/hash/mimc"
	"github.com/consensys/gnark/test"

	zkcrypto "sparkdream/tools/crypto"
)

// smallShieldCircuit is a test-sized circuit with depth 4 (supports 16 members).
type smallShieldCircuit struct {
	MerkleRoot         frontend.Variable `gnark:",public"`
	Nullifier          frontend.Variable `gnark:",public"`
	RateLimitNullifier frontend.Variable `gnark:",public"`
	MinTrustLevel      frontend.Variable `gnark:",public"`
	Scope              frontend.Variable `gnark:",public"`
	RateLimitEpoch     frontend.Variable `gnark:",public"`

	SecretKey    frontend.Variable
	TrustLevel   frontend.Variable
	PathElements [4]frontend.Variable
	PathIndices  [4]frontend.Variable
}

func (c *smallShieldCircuit) Define(api frontend.API) error {
	hFunc, err := mimc.NewMiMC(api)
	if err != nil {
		return err
	}

	// 1. publicKey = H(secretKey)
	hFunc.Reset()
	hFunc.Write(c.SecretKey)
	publicKey := hFunc.Sum()

	// 2. leaf = H(publicKey, trustLevel)
	hFunc.Reset()
	hFunc.Write(publicKey)
	hFunc.Write(c.TrustLevel)
	leaf := hFunc.Sum()

	// 3. Merkle proof
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

	// 4. trustLevel >= minTrustLevel
	api.AssertIsLessOrEqual(c.MinTrustLevel, c.TrustLevel)

	// 5. nullifier = H(secretKey, scope)
	hFunc.Reset()
	hFunc.Write(c.SecretKey)
	hFunc.Write(c.Scope)
	expectedNullifier := hFunc.Sum()
	api.AssertIsEqual(expectedNullifier, c.Nullifier)

	// 6. rateLimitNullifier = H(secretKey, domainTag, rateLimitEpoch)
	hFunc.Reset()
	hFunc.Write(c.SecretKey)
	hFunc.Write(uint64(math.MaxUint64))
	hFunc.Write(c.RateLimitEpoch)
	expectedRLNullifier := hFunc.Sum()
	api.AssertIsEqual(expectedRLNullifier, c.RateLimitNullifier)

	// 7. path indices binary
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
		leaf := zkcrypto.ComputeLeaf(pk, trustLevels[i])
		tree.AddLeaf(leaf) //nolint:errcheck
	}
	if len(pubKeys) > 0 {
		tree.Build() //nolint:errcheck
	}
	return tree
}

// makeAssignment creates a valid test assignment for the small shield circuit.
func makeAssignment(
	secretKey []byte,
	trustLevel uint64,
	minTrustLevel uint64,
	scope uint64,
	rateLimitEpoch uint64,
	tree *zkcrypto.MerkleTree,
	leafIndex int,
) *smallShieldCircuit {
	proof, err := tree.GetProof(leafIndex)
	if err != nil {
		panic(err)
	}

	// Compute nullifiers
	nullifier := zkcrypto.ComputeNullifier(secretKey, scope)
	rlNullifier := zkcrypto.ComputeRateLimitNullifier(secretKey, rateLimitEpoch)

	assignment := &smallShieldCircuit{
		MerkleRoot:         new(big.Int).SetBytes(zkcrypto.PadTo32(tree.Root())),
		Nullifier:          new(big.Int).SetBytes(zkcrypto.PadTo32(nullifier)),
		RateLimitNullifier: new(big.Int).SetBytes(zkcrypto.PadTo32(rlNullifier)),
		MinTrustLevel:      minTrustLevel,
		Scope:              scope,
		RateLimitEpoch:     rateLimitEpoch,
		SecretKey:          new(big.Int).SetBytes(zkcrypto.PadTo32(secretKey)),
		TrustLevel:         trustLevel,
	}

	for i := 0; i < 4; i++ {
		assignment.PathElements[i] = new(big.Int).SetBytes(zkcrypto.PadTo32(proof.PathElements[i]))
		assignment.PathIndices[i] = proof.PathIndices[i]
	}

	return assignment
}

func TestShieldCircuit_ValidProof(t *testing.T) {
	secretKey := zkcrypto.PadTo32([]byte("test-secret-key-valid-shield"))
	publicKey := zkcrypto.DerivePublicKey(secretKey)
	trustLevel := uint64(2) // ESTABLISHED
	scope := uint64(42)     // e.g., epoch number
	rlEpoch := uint64(42)   // same epoch for rate limit

	// Build tree with 3 members
	otherPK1 := zkcrypto.DerivePublicKey(zkcrypto.PadTo32([]byte("other-member-1")))
	otherPK2 := zkcrypto.DerivePublicKey(zkcrypto.PadTo32([]byte("other-member-2")))

	pubKeys := [][]byte{publicKey, otherPK1, otherPK2}
	trustLevels := []uint64{trustLevel, 1, 3}
	tree := buildSmallTrustTree(pubKeys, trustLevels)

	leaf := zkcrypto.ComputeLeaf(publicKey, trustLevel)
	leafIndex := tree.FindLeafIndex(leaf)
	if leafIndex < 0 {
		t.Fatal("leaf not found in tree")
	}

	assignment := makeAssignment(secretKey, trustLevel, 1, scope, rlEpoch, tree, leafIndex)

	assert := test.NewAssert(t)
	assert.ProverSucceeded(&smallShieldCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func TestShieldCircuit_InsufficientTrustLevel(t *testing.T) {
	secretKey := zkcrypto.PadTo32([]byte("test-secret-key-insufficient"))
	publicKey := zkcrypto.DerivePublicKey(secretKey)
	trustLevel := uint64(0) // NEW
	scope := uint64(42)
	rlEpoch := uint64(42)

	tree := buildSmallTrustTree([][]byte{publicKey}, []uint64{trustLevel})
	leaf := zkcrypto.ComputeLeaf(publicKey, trustLevel)

	assignment := makeAssignment(secretKey, trustLevel, 1, scope, rlEpoch, tree, tree.FindLeafIndex(leaf))

	assert := test.NewAssert(t)
	assert.ProverFailed(&smallShieldCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func TestShieldCircuit_WrongScope(t *testing.T) {
	secretKey := zkcrypto.PadTo32([]byte("test-secret-key-wrong-scope"))
	publicKey := zkcrypto.DerivePublicKey(secretKey)
	trustLevel := uint64(2)
	correctScope := uint64(42)
	wrongScope := uint64(99)
	rlEpoch := uint64(42)

	tree := buildSmallTrustTree([][]byte{publicKey}, []uint64{trustLevel})
	leaf := zkcrypto.ComputeLeaf(publicKey, trustLevel)
	leafIndex := tree.FindLeafIndex(leaf)

	// Build assignment with correct scope but wrong nullifier (computed with wrongScope)
	proof, _ := tree.GetProof(leafIndex)
	wrongNullifier := zkcrypto.ComputeNullifier(secretKey, wrongScope)
	rlNullifier := zkcrypto.ComputeRateLimitNullifier(secretKey, rlEpoch)

	assignment := &smallShieldCircuit{
		MerkleRoot:         new(big.Int).SetBytes(zkcrypto.PadTo32(tree.Root())),
		Nullifier:          new(big.Int).SetBytes(zkcrypto.PadTo32(wrongNullifier)),
		RateLimitNullifier: new(big.Int).SetBytes(zkcrypto.PadTo32(rlNullifier)),
		MinTrustLevel:      uint64(1),
		Scope:              correctScope, // Scope doesn't match the nullifier
		RateLimitEpoch:     rlEpoch,
		SecretKey:          new(big.Int).SetBytes(zkcrypto.PadTo32(secretKey)),
		TrustLevel:         trustLevel,
	}

	for i := 0; i < 4; i++ {
		assignment.PathElements[i] = new(big.Int).SetBytes(zkcrypto.PadTo32(proof.PathElements[i]))
		assignment.PathIndices[i] = proof.PathIndices[i]
	}

	assert := test.NewAssert(t)
	assert.ProverFailed(&smallShieldCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func TestShieldCircuit_WrongMerkleRoot(t *testing.T) {
	secretKey := zkcrypto.PadTo32([]byte("test-secret-key-wrong-root"))
	publicKey := zkcrypto.DerivePublicKey(secretKey)
	trustLevel := uint64(2)
	scope := uint64(42)
	rlEpoch := uint64(42)

	tree := buildSmallTrustTree([][]byte{publicKey}, []uint64{trustLevel})
	leaf := zkcrypto.ComputeLeaf(publicKey, trustLevel)
	leafIndex := tree.FindLeafIndex(leaf)

	proof, _ := tree.GetProof(leafIndex)
	nullifier := zkcrypto.ComputeNullifier(secretKey, scope)
	rlNullifier := zkcrypto.ComputeRateLimitNullifier(secretKey, rlEpoch)
	fakeRoot := zkcrypto.HashToField([]byte("fake-root"))

	assignment := &smallShieldCircuit{
		MerkleRoot:         new(big.Int).SetBytes(zkcrypto.PadTo32(fakeRoot)),
		Nullifier:          new(big.Int).SetBytes(zkcrypto.PadTo32(nullifier)),
		RateLimitNullifier: new(big.Int).SetBytes(zkcrypto.PadTo32(rlNullifier)),
		MinTrustLevel:      uint64(1),
		Scope:              scope,
		RateLimitEpoch:     rlEpoch,
		SecretKey:          new(big.Int).SetBytes(zkcrypto.PadTo32(secretKey)),
		TrustLevel:         trustLevel,
	}

	for i := 0; i < 4; i++ {
		assignment.PathElements[i] = new(big.Int).SetBytes(zkcrypto.PadTo32(proof.PathElements[i]))
		assignment.PathIndices[i] = proof.PathIndices[i]
	}

	assert := test.NewAssert(t)
	assert.ProverFailed(&smallShieldCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func TestShieldCircuit_ForgedRateLimitNullifier(t *testing.T) {
	// Prove that a forged (random) rate limit nullifier is rejected
	secretKey := zkcrypto.PadTo32([]byte("test-secret-key-forged-rl"))
	publicKey := zkcrypto.DerivePublicKey(secretKey)
	trustLevel := uint64(2)
	scope := uint64(42)
	rlEpoch := uint64(42)

	tree := buildSmallTrustTree([][]byte{publicKey}, []uint64{trustLevel})
	leaf := zkcrypto.ComputeLeaf(publicKey, trustLevel)
	leafIndex := tree.FindLeafIndex(leaf)

	proof, _ := tree.GetProof(leafIndex)
	nullifier := zkcrypto.ComputeNullifier(secretKey, scope)
	// Use a forged rate limit nullifier instead of the real one
	forgedRLNullifier := zkcrypto.HashToField([]byte("forged-rate-limit-nullifier"))

	assignment := &smallShieldCircuit{
		MerkleRoot:         new(big.Int).SetBytes(zkcrypto.PadTo32(tree.Root())),
		Nullifier:          new(big.Int).SetBytes(zkcrypto.PadTo32(nullifier)),
		RateLimitNullifier: new(big.Int).SetBytes(zkcrypto.PadTo32(forgedRLNullifier)),
		MinTrustLevel:      uint64(1),
		Scope:              scope,
		RateLimitEpoch:     rlEpoch,
		SecretKey:          new(big.Int).SetBytes(zkcrypto.PadTo32(secretKey)),
		TrustLevel:         trustLevel,
	}

	for i := 0; i < 4; i++ {
		assignment.PathElements[i] = new(big.Int).SetBytes(zkcrypto.PadTo32(proof.PathElements[i]))
		assignment.PathIndices[i] = proof.PathIndices[i]
	}

	assert := test.NewAssert(t)
	assert.ProverFailed(&smallShieldCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func TestShieldCircuit_WrongRateLimitEpoch(t *testing.T) {
	// Rate limit nullifier computed for epoch 42 but verifier checks epoch 43
	secretKey := zkcrypto.PadTo32([]byte("test-secret-key-wrong-rl-epoch"))
	publicKey := zkcrypto.DerivePublicKey(secretKey)
	trustLevel := uint64(2)
	scope := uint64(42)
	clientEpoch := uint64(42)
	verifierEpoch := uint64(43) // Different epoch

	tree := buildSmallTrustTree([][]byte{publicKey}, []uint64{trustLevel})
	leaf := zkcrypto.ComputeLeaf(publicKey, trustLevel)
	leafIndex := tree.FindLeafIndex(leaf)

	proof, _ := tree.GetProof(leafIndex)
	nullifier := zkcrypto.ComputeNullifier(secretKey, scope)
	// Client computes rate limit nullifier for epoch 42
	rlNullifier := zkcrypto.ComputeRateLimitNullifier(secretKey, clientEpoch)

	assignment := &smallShieldCircuit{
		MerkleRoot:         new(big.Int).SetBytes(zkcrypto.PadTo32(tree.Root())),
		Nullifier:          new(big.Int).SetBytes(zkcrypto.PadTo32(nullifier)),
		RateLimitNullifier: new(big.Int).SetBytes(zkcrypto.PadTo32(rlNullifier)),
		MinTrustLevel:      uint64(1),
		Scope:              scope,
		RateLimitEpoch:     verifierEpoch, // Verifier uses epoch 43
		SecretKey:          new(big.Int).SetBytes(zkcrypto.PadTo32(secretKey)),
		TrustLevel:         trustLevel,
	}

	for i := 0; i < 4; i++ {
		assignment.PathElements[i] = new(big.Int).SetBytes(zkcrypto.PadTo32(proof.PathElements[i]))
		assignment.PathIndices[i] = proof.PathIndices[i]
	}

	assert := test.NewAssert(t)
	assert.ProverFailed(&smallShieldCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func TestShieldCircuit_DifferentScopesDifferentNullifiers(t *testing.T) {
	// Verify that different scopes produce different nullifiers (unlinkability)
	secretKey := zkcrypto.PadTo32([]byte("test-secret-key-unlinkable"))
	publicKey := zkcrypto.DerivePublicKey(secretKey)
	trustLevel := uint64(2)
	rlEpoch := uint64(42)

	tree := buildSmallTrustTree([][]byte{publicKey}, []uint64{trustLevel})
	leaf := zkcrypto.ComputeLeaf(publicKey, trustLevel)
	leafIndex := tree.FindLeafIndex(leaf)

	// Both scopes should produce valid proofs with different nullifiers
	for _, scope := range []uint64{100, 200} {
		assignment := makeAssignment(secretKey, trustLevel, 1, scope, rlEpoch, tree, leafIndex)

		assert := test.NewAssert(t)
		assert.ProverSucceeded(&smallShieldCircuit{}, assignment, test.WithCurves(ecc.BN254))
	}

	// Verify nullifiers are actually different
	null1 := zkcrypto.ComputeNullifier(secretKey, 100)
	null2 := zkcrypto.ComputeNullifier(secretKey, 200)
	if new(big.Int).SetBytes(null1).Cmp(new(big.Int).SetBytes(null2)) == 0 {
		t.Fatal("different scopes must produce different nullifiers")
	}

	// But rate limit nullifiers should be the same (same identity, same epoch)
	rl1 := zkcrypto.ComputeRateLimitNullifier(secretKey, rlEpoch)
	rl2 := zkcrypto.ComputeRateLimitNullifier(secretKey, rlEpoch)
	if new(big.Int).SetBytes(rl1).Cmp(new(big.Int).SetBytes(rl2)) != 0 {
		t.Fatal("same identity and epoch must produce same rate limit nullifier")
	}
}
