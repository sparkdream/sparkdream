// Package circuit implements a ZK-SNARK circuit for anonymous voting.
//
// The circuit proves that a voter:
// 1. Is a member of the eligible voter set (Merkle proof)
// 2. Has computed a valid nullifier (prevents double voting)
// 3. Is voting with the correct voting power
// 4. Is casting a valid vote option
//
// All of this is proven WITHOUT revealing the voter's identity.
package circuit

import (
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/hash/mimc"
)

// TreeDepth defines the depth of the voter Merkle tree.
// A depth of 20 supports up to 2^20 = 1,048,576 voters.
// Adjust based on your expected voter count:
//   - 10: ~1,000 voters
//   - 15: ~32,000 voters
//   - 20: ~1,000,000 voters
//   - 25: ~33,000,000 voters
const TreeDepth = 20

// VoteCircuit defines the ZK circuit for anonymous voting.
//
// PUBLIC INPUTS (revealed on-chain):
//   - MerkleRoot: The root of the voter eligibility Merkle tree
//   - Nullifier: A unique value derived from (secretKey, proposalID) that prevents double voting
//   - ProposalID: The ID of the proposal being voted on
//   - VoteOption: The vote choice (0=yes, 1=no, 2=abstain)
//   - VotingPower: The voter's voting power
//
// PRIVATE INPUTS (known only to the voter):
//   - SecretKey: The voter's secret key (never revealed)
//   - PathElements: The sibling hashes along the Merkle proof path
//   - PathIndices: The position (left=0, right=1) at each level of the proof
type VoteCircuit struct {
	// === PUBLIC INPUTS ===
	// These are visible on-chain and used to verify the vote

	// MerkleRoot is the root of the voter Merkle tree, snapshotted when the proposal was created
	MerkleRoot frontend.Variable `gnark:",public"`

	// Nullifier = hash(secretKey, proposalID)
	// This prevents double voting: the same voter voting twice would produce the same nullifier
	Nullifier frontend.Variable `gnark:",public"`

	// ProposalID identifies which proposal is being voted on
	ProposalID frontend.Variable `gnark:",public"`

	// VoteOption is the vote: 0=yes, 1=no, 2=abstain
	VoteOption frontend.Variable `gnark:",public"`

	// VotingPower is how much voting power this vote carries
	VotingPower frontend.Variable `gnark:",public"`

	// === PRIVATE INPUTS (Witness) ===
	// These are known only to the voter and never revealed

	// SecretKey is the voter's private key, used to derive their public key and nullifier
	SecretKey frontend.Variable

	// PathElements are the sibling hashes in the Merkle proof
	PathElements [TreeDepth]frontend.Variable

	// PathIndices indicate whether we're the left (0) or right (1) child at each level
	PathIndices [TreeDepth]frontend.Variable
}

// Define declares the circuit's constraints.
// This is called by gnark during circuit compilation.
func (c *VoteCircuit) Define(api frontend.API) error {
	// Initialize MiMC hash function
	// MiMC is a SNARK-friendly hash function with ~300 constraints per hash
	hFunc, err := mimc.NewMiMC(api)
	if err != nil {
		return err
	}

	// =========================================================================
	// CONSTRAINT 1: Compute public key from secret key
	// publicKey = hash(secretKey)
	//
	// The public key is what's stored in the Merkle tree. By hashing the secret
	// key, we ensure the secret key is never revealed, even in the Merkle tree.
	// =========================================================================
	hFunc.Reset()
	hFunc.Write(c.SecretKey)
	publicKey := hFunc.Sum()

	// =========================================================================
	// CONSTRAINT 2: Compute the leaf value
	// leaf = hash(publicKey, votingPower)
	//
	// Each leaf in the Merkle tree represents a voter and their voting power.
	// This binding ensures a voter can't claim more power than they have.
	// =========================================================================
	hFunc.Reset()
	hFunc.Write(publicKey)
	hFunc.Write(c.VotingPower)
	leaf := hFunc.Sum()

	// =========================================================================
	// CONSTRAINT 3: Verify Merkle proof
	// Proves the computed leaf is in the tree with the given root.
	//
	// We traverse from the leaf to the root, hashing with siblings along the way.
	// If the computed root matches MerkleRoot, the voter is in the tree.
	// =========================================================================
	computedRoot := c.verifyMerkleProof(api, hFunc, leaf)
	api.AssertIsEqual(computedRoot, c.MerkleRoot)

	// =========================================================================
	// CONSTRAINT 4: Verify nullifier computation
	// nullifier = hash(secretKey, proposalID)
	//
	// The nullifier uniquely identifies this voter's vote on this proposal.
	// - Same voter + same proposal = same nullifier (prevents double voting)
	// - Same voter + different proposal = different nullifier (unlinkable across proposals)
	// - Different voter = different nullifier (can't impersonate)
	//
	// The nullifier is published on-chain, but cannot be linked to the voter's
	// identity because computing it requires knowledge of the secret key.
	// =========================================================================
	hFunc.Reset()
	hFunc.Write(c.SecretKey)
	hFunc.Write(c.ProposalID)
	expectedNullifier := hFunc.Sum()
	api.AssertIsEqual(expectedNullifier, c.Nullifier)

	// =========================================================================
	// CONSTRAINT 5: Verify vote option is valid
	// VoteOption must be 0 (yes), 1 (no), or 2 (abstain)
	//
	// We use the polynomial constraint: x * (x-1) * (x-2) = 0
	// This is satisfied if and only if x ∈ {0, 1, 2}
	// =========================================================================
	optionMinus1 := api.Sub(c.VoteOption, 1)
	optionMinus2 := api.Sub(c.VoteOption, 2)
	product := api.Mul(c.VoteOption, optionMinus1)
	product = api.Mul(product, optionMinus2)
	api.AssertIsEqual(product, 0)

	// =========================================================================
	// CONSTRAINT 6: Verify path indices are binary (0 or 1)
	// Each pathIndex must be 0 or 1, not any other field element
	//
	// We use: x * (x - 1) = 0, which is true only for x ∈ {0, 1}
	// =========================================================================
	for i := 0; i < TreeDepth; i++ {
		c.assertIsBinary(api, c.PathIndices[i])
	}

	return nil
}

// verifyMerkleProof computes the Merkle root from a leaf and proof path.
// Returns the computed root, which should match the expected MerkleRoot.
func (c *VoteCircuit) verifyMerkleProof(
	api frontend.API,
	hFunc mimc.MiMC,
	leaf frontend.Variable,
) frontend.Variable {
	current := leaf

	for i := 0; i < TreeDepth; i++ {
		// At each level, we need to hash (left, right) to get the parent
		// PathIndices[i] tells us if we're the left child (0) or right child (1)

		// Get the sibling from the proof
		sibling := c.PathElements[i]

		// Compute both possible orderings:
		// If we're left child: hash(current, sibling)
		// If we're right child: hash(sibling, current)
		hFunc.Reset()
		hFunc.Write(current)
		hFunc.Write(sibling)
		hashAsLeft := hFunc.Sum()

		hFunc.Reset()
		hFunc.Write(sibling)
		hFunc.Write(current)
		hashAsRight := hFunc.Sum()

		// Select the correct hash based on PathIndices[i]
		// api.Select(condition, ifTrue, ifFalse)
		// If PathIndices[i] == 1 (we're right child), use hashAsRight
		// If PathIndices[i] == 0 (we're left child), use hashAsLeft
		current = api.Select(c.PathIndices[i], hashAsRight, hashAsLeft)
	}

	return current
}

// assertIsBinary asserts that a variable is either 0 or 1.
// Uses the constraint: x * (x - 1) = 0
func (c *VoteCircuit) assertIsBinary(api frontend.API, v frontend.Variable) {
	vMinus1 := api.Sub(v, 1)
	product := api.Mul(v, vMinus1)
	api.AssertIsEqual(product, 0)
}

// NumConstraints returns an estimate of the number of constraints in the circuit.
// Useful for benchmarking and gas estimation.
// Actual count may vary slightly based on gnark optimizations.
func NumConstraints() int {
	// Per MiMC hash: ~300 constraints (for BN254)
	// We have:
	// - 1 hash for public key
	// - 1 hash for leaf
	// - TreeDepth * 2 hashes for Merkle proof (we compute both orderings)
	// - 1 hash for nullifier
	// Plus:
	// - ~5 constraints for vote option check
	// - TreeDepth constraints for binary checks
	// - TreeDepth select operations (~3 each)

	hashConstraints := 300
	numHashes := 1 + 1 + (TreeDepth * 2) + 1 // pubkey + leaf + merkle + nullifier
	otherConstraints := 5 + TreeDepth + (TreeDepth * 3)

	return (numHashes * hashConstraints) + otherConstraints
}
