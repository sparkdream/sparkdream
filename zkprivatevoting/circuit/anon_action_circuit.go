// Package circuit implements ZK-SNARK circuits for anonymous actions.
//
// AnonActionCircuit proves that a user:
// 1. Is a member of the trust tree (Merkle proof over trust-level-encoded leaves)
// 2. Meets a minimum trust level requirement
// 3. Has computed a valid scoped nullifier (prevents double actions per scope)
//
// All of this is proven WITHOUT revealing the member's identity or exact trust level.
package circuit

import (
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/hash/mimc"
)

// AnonActionCircuit defines the ZK circuit for anonymous actions (posting, replying, etc.).
//
// PUBLIC INPUTS (revealed on-chain):
//   - MerkleRoot: The root of the member trust Merkle tree
//   - Nullifier: A unique value derived from (secretKey, scope) that prevents double actions
//   - MinTrustLevel: The minimum trust level required for this action
//   - Scope: Context binding (e.g., epoch for posts, postID for replies)
//
// PRIVATE INPUTS (known only to the prover):
//   - SecretKey: The member's secret key (never revealed)
//   - TrustLevel: The member's actual trust level
//   - PathElements: The sibling hashes along the Merkle proof path
//   - PathIndices: The position (left=0, right=1) at each level of the proof
type AnonActionCircuit struct {
	// === PUBLIC INPUTS ===
	// These are visible on-chain and used to verify the proof

	// MerkleRoot is the root of the member trust tree, maintained by x/rep
	MerkleRoot frontend.Variable `gnark:",public"`

	// Nullifier = hash(secretKey, scope)
	// Prevents double actions: same member acting twice in same scope produces same nullifier
	Nullifier frontend.Variable `gnark:",public"`

	// MinTrustLevel is the minimum trust level required for this action
	MinTrustLevel frontend.Variable `gnark:",public"`

	// Scope binds the nullifier to a context (epoch number, post ID, etc.)
	Scope frontend.Variable `gnark:",public"`

	// === PRIVATE INPUTS (Witness) ===
	// These are known only to the prover and never revealed

	// SecretKey is the member's private key, used to derive their public key and nullifier
	SecretKey frontend.Variable

	// TrustLevel is the member's actual trust level (encoded in the leaf)
	TrustLevel frontend.Variable

	// PathElements are the sibling hashes in the Merkle proof
	PathElements [TreeDepth]frontend.Variable

	// PathIndices indicate whether we're the left (0) or right (1) child at each level
	PathIndices [TreeDepth]frontend.Variable
}

// Define declares the circuit's constraints.
// This is called by gnark during circuit compilation.
func (c *AnonActionCircuit) Define(api frontend.API) error {
	// Initialize MiMC hash function
	hFunc, err := mimc.NewMiMC(api)
	if err != nil {
		return err
	}

	// =========================================================================
	// CONSTRAINT 1: Compute public key from secret key
	// publicKey = hash(secretKey)
	//
	// The public key is what's stored in the Merkle tree leaves.
	// =========================================================================
	hFunc.Reset()
	hFunc.Write(c.SecretKey)
	publicKey := hFunc.Sum()

	// =========================================================================
	// CONSTRAINT 2: Compute the leaf value
	// leaf = hash(publicKey, trustLevel)
	//
	// Each leaf in the trust tree represents a member and their trust level.
	// Unlike the vote circuit which uses votingPower, this uses trustLevel.
	// =========================================================================
	hFunc.Reset()
	hFunc.Write(publicKey)
	hFunc.Write(c.TrustLevel)
	leaf := hFunc.Sum()

	// =========================================================================
	// CONSTRAINT 3: Verify Merkle proof
	// Proves the computed leaf is in the tree with the given root.
	// =========================================================================
	computedRoot := c.verifyMerkleProof(api, hFunc, leaf)
	api.AssertIsEqual(computedRoot, c.MerkleRoot)

	// =========================================================================
	// CONSTRAINT 4: Verify trust level meets minimum
	// trustLevel >= minTrustLevel
	//
	// We use api.Cmp which returns -1, 0, or 1.
	// We assert that (trustLevel - minTrustLevel) >= 0, i.e., the result is
	// not -1. We do this by checking that trustLevel >= minTrustLevel using
	// AssertIsLessOrEqual(minTrustLevel, trustLevel).
	// =========================================================================
	api.AssertIsLessOrEqual(c.MinTrustLevel, c.TrustLevel)

	// =========================================================================
	// CONSTRAINT 5: Verify nullifier computation
	// nullifier = hash(secretKey, scope)
	//
	// The nullifier uniquely identifies this member's action in this scope.
	// - Same member + same scope = same nullifier (prevents double action)
	// - Same member + different scope = different nullifier (unlinkable across scopes)
	// - Different member = different nullifier (can't impersonate)
	// =========================================================================
	hFunc.Reset()
	hFunc.Write(c.SecretKey)
	hFunc.Write(c.Scope)
	expectedNullifier := hFunc.Sum()
	api.AssertIsEqual(expectedNullifier, c.Nullifier)

	// =========================================================================
	// CONSTRAINT 6: Verify path indices are binary (0 or 1)
	// Each pathIndex must be 0 or 1, not any other field element
	// =========================================================================
	for i := 0; i < TreeDepth; i++ {
		c.assertIsBinary(api, c.PathIndices[i])
	}

	return nil
}

// verifyMerkleProof computes the Merkle root from a leaf and proof path.
func (c *AnonActionCircuit) verifyMerkleProof(
	api frontend.API,
	hFunc mimc.MiMC,
	leaf frontend.Variable,
) frontend.Variable {
	current := leaf

	for i := 0; i < TreeDepth; i++ {
		sibling := c.PathElements[i]

		hFunc.Reset()
		hFunc.Write(current)
		hFunc.Write(sibling)
		hashAsLeft := hFunc.Sum()

		hFunc.Reset()
		hFunc.Write(sibling)
		hFunc.Write(current)
		hashAsRight := hFunc.Sum()

		// If PathIndices[i] == 1 (we're right child), use hashAsRight
		// If PathIndices[i] == 0 (we're left child), use hashAsLeft
		current = api.Select(c.PathIndices[i], hashAsRight, hashAsLeft)
	}

	return current
}

// assertIsBinary asserts that a variable is either 0 or 1.
func (c *AnonActionCircuit) assertIsBinary(api frontend.API, v frontend.Variable) {
	vMinus1 := api.Sub(v, 1)
	product := api.Mul(v, vMinus1)
	api.AssertIsEqual(product, 0)
}

// AnonActionNumConstraints returns an estimate of the number of constraints.
func AnonActionNumConstraints() int {
	hashConstraints := 300
	// pubkey + leaf + merkle (2 per level) + nullifier
	numHashes := 1 + 1 + (TreeDepth * 2) + 1
	// trust level comparison + binary checks + select operations
	otherConstraints := 10 + TreeDepth + (TreeDepth * 3)

	return (numHashes * hashConstraints) + otherConstraints
}
