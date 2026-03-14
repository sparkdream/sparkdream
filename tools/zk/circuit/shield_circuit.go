// Package circuit implements ZK-SNARK circuits for anonymous shielded operations.
//
// ShieldCircuit is the unified circuit used by x/shield for all anonymous actions.
// It proves that a member:
//  1. Is in the trust tree (Merkle proof over trust-level-encoded leaves)
//  2. Meets a minimum trust level requirement
//  3. Has computed a valid scoped nullifier (prevents double actions)
//  4. Has computed a valid rate-limit nullifier (enables per-identity rate limiting)
//
// All of this is proven WITHOUT revealing the member's identity or exact trust level.
package circuit

import (
	"math"

	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/hash/mimc"
)

// TreeDepth defines the depth of the trust Merkle tree.
// A depth of 20 supports up to 2^20 = 1,048,576 members.
const TreeDepth = 20

// RateLimitDomainTag is a constant baked into the circuit that domain-separates
// rate-limit nullifiers from action nullifiers. Using MaxUint64 ensures no
// collision with realistic scope values (which are small sequential integers).
//
// Action nullifier:     H(secretKey, scope)           — 2 inputs
// Rate-limit nullifier: H(secretKey, domainTag, epoch) — 3 inputs with domainTag = MaxUint64
//
// Both the circuit and client-side crypto.ComputeRateLimitNullifier() must use
// this exact value.
const RateLimitDomainTag = math.MaxUint64

// ShieldCircuit defines the unified ZK circuit for all anonymous shielded operations.
//
// PUBLIC INPUTS (revealed on-chain):
//   - MerkleRoot: Root of the member trust Merkle tree (maintained by x/rep)
//   - Nullifier: H(secretKey, scope) — prevents double actions per scope
//   - RateLimitNullifier: H(secretKey, domainTag, rateLimitEpoch) — per-identity rate limiting
//   - MinTrustLevel: Minimum trust level required for this action
//   - Scope: Context binding (epoch, postID, proposalID, etc.)
//   - RateLimitEpoch: Epoch for rate-limit binding (set by verifier)
//
// PRIVATE INPUTS (known only to the prover):
//   - SecretKey: The member's secret key (never revealed)
//   - TrustLevel: The member's actual trust level
//   - PathElements: Sibling hashes along the Merkle proof path
//   - PathIndices: Position (left=0, right=1) at each level of the proof
type ShieldCircuit struct {
	// === PUBLIC INPUTS ===

	// MerkleRoot is the root of the member trust tree, maintained by x/rep
	MerkleRoot frontend.Variable `gnark:",public"`

	// Nullifier = H(secretKey, scope)
	// Prevents double actions: same member acting twice in same scope produces same nullifier
	Nullifier frontend.Variable `gnark:",public"`

	// RateLimitNullifier = H(secretKey, RateLimitDomainTag, rateLimitEpoch)
	// Enables per-identity rate limiting without breaking anonymity.
	// The domain tag separates this from action nullifiers.
	RateLimitNullifier frontend.Variable `gnark:",public"`

	// MinTrustLevel is the minimum trust level required for this action
	MinTrustLevel frontend.Variable `gnark:",public"`

	// Scope binds the action nullifier to a context (epoch, post ID, proposal ID, etc.)
	Scope frontend.Variable `gnark:",public"`

	// RateLimitEpoch binds the rate-limit nullifier to the current epoch
	RateLimitEpoch frontend.Variable `gnark:",public"`

	// === PRIVATE INPUTS (Witness) ===

	// SecretKey is the member's private key, used to derive their public key and nullifiers
	SecretKey frontend.Variable

	// TrustLevel is the member's actual trust level (encoded in the Merkle leaf)
	TrustLevel frontend.Variable

	// PathElements are the sibling hashes in the Merkle proof
	PathElements [TreeDepth]frontend.Variable

	// PathIndices indicate whether we're the left (0) or right (1) child at each level
	PathIndices [TreeDepth]frontend.Variable
}

// Define declares the circuit's constraints.
func (c *ShieldCircuit) Define(api frontend.API) error {
	hFunc, err := mimc.NewMiMC(api)
	if err != nil {
		return err
	}

	// =========================================================================
	// CONSTRAINT 1: Compute public key from secret key
	// publicKey = H(secretKey)
	// =========================================================================
	hFunc.Reset()
	hFunc.Write(c.SecretKey)
	publicKey := hFunc.Sum()

	// =========================================================================
	// CONSTRAINT 2: Compute the leaf value
	// leaf = H(publicKey, trustLevel)
	//
	// Each leaf in the trust tree represents a member and their trust level.
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
	// Only the lower bound is proven — the member's exact trust level is hidden.
	// =========================================================================
	api.AssertIsLessOrEqual(c.MinTrustLevel, c.TrustLevel)

	// =========================================================================
	// CONSTRAINT 5: Verify action nullifier computation
	// nullifier = H(secretKey, scope)
	//
	// The nullifier uniquely identifies this member's action in this scope.
	// - Same member + same scope = same nullifier (prevents double action)
	// - Same member + different scope = different nullifier (unlinkable)
	// - Different member = different nullifier (can't impersonate)
	// =========================================================================
	hFunc.Reset()
	hFunc.Write(c.SecretKey)
	hFunc.Write(c.Scope)
	expectedNullifier := hFunc.Sum()
	api.AssertIsEqual(expectedNullifier, c.Nullifier)

	// =========================================================================
	// CONSTRAINT 6: Verify rate-limit nullifier computation
	// rateLimitNullifier = H(secretKey, RateLimitDomainTag, rateLimitEpoch)
	//
	// This produces the same value for ALL operations by the same member in
	// the same epoch, enabling per-identity rate limiting without revealing
	// identity. The domain tag (MaxUint64) separates this from action nullifiers.
	// =========================================================================
	hFunc.Reset()
	hFunc.Write(c.SecretKey)
	hFunc.Write(uint64(RateLimitDomainTag))
	hFunc.Write(c.RateLimitEpoch)
	expectedRLNullifier := hFunc.Sum()
	api.AssertIsEqual(expectedRLNullifier, c.RateLimitNullifier)

	// =========================================================================
	// CONSTRAINT 7: Verify path indices are binary (0 or 1)
	// Each pathIndex must be 0 or 1, not any other field element.
	// Uses: x * (x - 1) = 0, which is true only for x ∈ {0, 1}
	// =========================================================================
	for i := 0; i < TreeDepth; i++ {
		c.assertIsBinary(api, c.PathIndices[i])
	}

	return nil
}

// verifyMerkleProof computes the Merkle root from a leaf and proof path.
func (c *ShieldCircuit) verifyMerkleProof(
	api frontend.API,
	hFunc mimc.MiMC,
	leaf frontend.Variable,
) frontend.Variable {
	current := leaf

	for i := 0; i < TreeDepth; i++ {
		sibling := c.PathElements[i]

		// Compute both possible orderings
		hFunc.Reset()
		hFunc.Write(current)
		hFunc.Write(sibling)
		hashAsLeft := hFunc.Sum()

		hFunc.Reset()
		hFunc.Write(sibling)
		hFunc.Write(current)
		hashAsRight := hFunc.Sum()

		// Select based on PathIndices[i]: 0=left child, 1=right child
		current = api.Select(c.PathIndices[i], hashAsRight, hashAsLeft)
	}

	return current
}

// assertIsBinary asserts that a variable is either 0 or 1.
func (c *ShieldCircuit) assertIsBinary(api frontend.API, v frontend.Variable) {
	vMinus1 := api.Sub(v, 1)
	product := api.Mul(v, vMinus1)
	api.AssertIsEqual(product, 0)
}

// ShieldNumConstraints returns an estimate of the number of constraints.
func ShieldNumConstraints() int {
	hashConstraints := 300
	// pubkey + leaf + merkle (2 per level) + action nullifier + rate limit nullifier (3 inputs)
	numHashes := 1 + 1 + (TreeDepth * 2) + 1 + 1
	// trust level comparison + binary checks + select operations
	otherConstraints := 10 + TreeDepth + (TreeDepth * 3)

	return (numHashes * hashConstraints) + otherConstraints
}
