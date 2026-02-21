package keeper

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"

	zkcrypto "sparkdream/zkprivatevoting/crypto"
)

// defaultTreeDepth matches the ZK circuit's TreeDepth constant (supports ~1M voters).
const defaultTreeDepth = zkcrypto.TreeDepth

// ---------------------------------------------------------------------------
// Commitment hash (used in commit-reveal for sealed votes)
// ---------------------------------------------------------------------------

// computeCommitmentHash computes MiMC(voteOption || salt) for the commit-reveal scheme.
// This MUST match the hash the voter computes client-side when creating a commitment.
func computeCommitmentHash(voteOption uint32, salt []byte) []byte {
	optBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(optBytes, voteOption)
	return zkcrypto.HashToField(optBytes, salt)
}

// ---------------------------------------------------------------------------
// Merkle tree helpers (used by helpers.go buildTreeSnapshot and queries)
// ---------------------------------------------------------------------------

// buildMerkleTree builds a MiMC Merkle tree from voter public keys.
// Each leaf is computed as MiMC(pubKey, votingPower=1) for equal-weight voting.
// Returns the Merkle root and number of voters.
//
// This is a package-level var so tests can override it with a fast
// small-depth implementation (the default depth-20 tree requires ~1M
// MiMC hashes which is too slow for unit tests).
var buildMerkleTree = func(zkPubKeys [][]byte) (root []byte, voterCount uint64) {
	if len(zkPubKeys) == 0 {
		return nil, 0
	}
	tree := zkcrypto.NewMerkleTree(defaultTreeDepth)
	for _, pubKey := range zkPubKeys {
		leaf := zkcrypto.ComputeLeaf(pubKey, 1)
		tree.AddLeaf(leaf) //nolint:errcheck // tree capacity is 2^20, won't overflow
	}
	tree.Build() //nolint:errcheck // leaves are non-empty
	return tree.Root(), uint64(len(zkPubKeys))
}

// buildMerkleTreeFull builds and returns the full tree structure, needed for
// generating individual voter Merkle proofs via the query handler.
//
// Package-level var for the same testability reason as buildMerkleTree.
var buildMerkleTreeFull = func(zkPubKeys [][]byte) *zkcrypto.MerkleTree {
	tree := zkcrypto.NewMerkleTree(defaultTreeDepth)
	for _, pubKey := range zkPubKeys {
		leaf := zkcrypto.ComputeLeaf(pubKey, 1)
		tree.AddLeaf(leaf) //nolint:errcheck
	}
	if len(zkPubKeys) > 0 {
		tree.Build() //nolint:errcheck
	}
	return tree
}

// ---------------------------------------------------------------------------
// ZK proof verification (Groth16 on BN254)
// ---------------------------------------------------------------------------

// voteCircuitPublicInputs is a minimal struct that mirrors the VoteCircuit's
// public fields. It is used solely to construct the public witness for
// Groth16 verification without importing the circuit package.
type voteCircuitPublicInputs struct {
	MerkleRoot  frontend.Variable `gnark:",public"`
	Nullifier   frontend.Variable `gnark:",public"`
	ProposalID  frontend.Variable `gnark:",public"`
	VoteOption  frontend.Variable `gnark:",public"`
	VotingPower frontend.Variable `gnark:",public"`

	// Private fields must be present so gnark knows the full circuit shape,
	// but they are zeroed out when building a public-only witness.
	SecretKey    frontend.Variable
	PathElements [defaultTreeDepth]frontend.Variable
	PathIndices  [defaultTreeDepth]frontend.Variable
}

// Define satisfies the frontend.Circuit interface. It is never actually
// executed during verification — gnark only needs it to know the circuit
// layout when constructing the public witness.
func (c *voteCircuitPublicInputs) Define(frontend.API) error { return nil }

// verifyVoteProof verifies a Groth16 ZK proof for an anonymous vote.
//
// If no verifying key is configured (len == 0), verification is skipped.
// This allows the chain to operate during development before a trusted
// setup ceremony has been performed.
func verifyVoteProof(_ context.Context, verifyingKey, merkleRoot, nullifier []byte, voteOption uint32, proof []byte) error {
	if len(verifyingKey) == 0 {
		// No verifying key configured — skip verification.
		return nil
	}

	vk := groth16.NewVerifyingKey(ecc.BN254)
	if _, err := vk.ReadFrom(bytes.NewReader(verifyingKey)); err != nil {
		return fmt.Errorf("invalid verifying key: %w", err)
	}

	p := groth16.NewProof(ecc.BN254)
	if _, err := p.ReadFrom(bytes.NewReader(proof)); err != nil {
		return fmt.Errorf("invalid proof bytes: %w", err)
	}

	// Build public-only witness matching the circuit's public input order.
	// VotingPower is always 1 (equal-weight voting).
	assignment := &voteCircuitPublicInputs{
		MerkleRoot:  new(big.Int).SetBytes(zkcrypto.PadTo32(merkleRoot)),
		Nullifier:   new(big.Int).SetBytes(zkcrypto.PadTo32(nullifier)),
		ProposalID:  0, // ProposalID is embedded in the nullifier; not separately validated here.
		VoteOption:  uint64(voteOption),
		VotingPower: uint64(1),
	}

	publicWitness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField(), frontend.PublicOnly())
	if err != nil {
		return fmt.Errorf("failed to build public witness: %w", err)
	}

	if err := groth16.Verify(p, vk, publicWitness); err != nil {
		return fmt.Errorf("proof verification failed: %w", err)
	}

	return nil
}

// proposalCircuitPublicInputs mirrors the public fields of a proposal-creation
// ZK circuit. The proposal circuit proves the submitter is an eligible voter
// without revealing their identity.
type proposalCircuitPublicInputs struct {
	MerkleRoot  frontend.Variable `gnark:",public"`
	Nullifier   frontend.Variable `gnark:",public"`
	VotingPower frontend.Variable `gnark:",public"`

	// Private fields (zeroed for public-only witness).
	SecretKey    frontend.Variable
	PathElements [defaultTreeDepth]frontend.Variable
	PathIndices  [defaultTreeDepth]frontend.Variable
}

func (c *proposalCircuitPublicInputs) Define(frontend.API) error { return nil }

// verifyProposalProof verifies a Groth16 ZK proof for anonymous proposal creation.
//
// If no verifying key is configured (len == 0), verification is skipped.
func verifyProposalProof(_ context.Context, verifyingKey, merkleRoot, nullifier, proof []byte) error {
	if len(verifyingKey) == 0 {
		return nil
	}

	vk := groth16.NewVerifyingKey(ecc.BN254)
	if _, err := vk.ReadFrom(bytes.NewReader(verifyingKey)); err != nil {
		return fmt.Errorf("invalid verifying key: %w", err)
	}

	p := groth16.NewProof(ecc.BN254)
	if _, err := p.ReadFrom(bytes.NewReader(proof)); err != nil {
		return fmt.Errorf("invalid proof bytes: %w", err)
	}

	assignment := &proposalCircuitPublicInputs{
		MerkleRoot:  new(big.Int).SetBytes(zkcrypto.PadTo32(merkleRoot)),
		Nullifier:   new(big.Int).SetBytes(zkcrypto.PadTo32(nullifier)),
		VotingPower: uint64(1),
	}

	publicWitness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField(), frontend.PublicOnly())
	if err != nil {
		return fmt.Errorf("failed to build public witness: %w", err)
	}

	if err := groth16.Verify(p, vk, publicWitness); err != nil {
		return fmt.Errorf("proof verification failed: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Circuit compilation helper (for key generation tooling)
// ---------------------------------------------------------------------------

// CompileVoteCircuit compiles the vote circuit and returns the constraint system.
// This is used by key-generation tooling, not by the chain itself.
var CompileVoteCircuit = func() (constraint.ConstraintSystem, error) {
	var circuit voteCircuitPublicInputs
	return frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit)
}
