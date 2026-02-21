// Package prover provides client-side proof generation for anonymous voting.
//
// This package is used by voters to generate ZK proofs for their votes.
// The proofs can then be submitted to the blockchain for verification.
//
// Usage:
//
//	prover, err := NewVoteProver("proving_key.bin", "circuit.r1cs")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	proof, err := prover.GenerateProof(input)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Submit proof.ProofBytes, proof.Nullifier, etc. to chain
package prover

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"

	"sparkdream/zkprivatevoting/circuit"
	"sparkdream/zkprivatevoting/crypto"
)

// VoteProver handles proof generation for anonymous votes
type VoteProver struct {
	provingKey groth16.ProvingKey
	ccs        constraint.ConstraintSystem
}

// VoteProofInput contains all inputs needed to generate a vote proof
type VoteProofInput struct {
	// SecretKey is the voter's secret key (32 bytes)
	SecretKey []byte

	// VotingPower is the voter's voting power (must match what's in the Merkle tree)
	VotingPower uint64

	// ProposalID is the ID of the proposal being voted on
	ProposalID uint64

	// VoteOption is the vote choice: 0=yes, 1=no, 2=abstain
	VoteOption uint8

	// MerkleRoot is the root of the voter eligibility tree
	MerkleRoot []byte

	// MerkleProof is the proof that the voter is in the tree
	MerkleProof *crypto.MerkleProof
}

// VoteProofOutput contains the generated proof and public inputs
type VoteProofOutput struct {
	// ProofBytes is the serialized ZK proof
	ProofBytes []byte

	// Nullifier prevents double voting (submit this on-chain)
	Nullifier []byte

	// ProposalID is the proposal being voted on
	ProposalID uint64

	// VoteOption is the vote choice
	VoteOption uint8

	// VotingPower is the voting power used
	VotingPower uint64

	// MerkleRoot should match the proposal's snapshot root
	MerkleRoot []byte

	// ProvingTime is how long proof generation took
	ProvingTime time.Duration
}

// NewVoteProver creates a new prover from key files
func NewVoteProver(provingKeyPath string, r1csPath string) (*VoteProver, error) {
	// Load proving key
	pkFile, err := os.Open(provingKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open proving key: %w", err)
	}
	defer pkFile.Close()

	pk := groth16.NewProvingKey(ecc.BN254)
	if _, err := pk.ReadFrom(pkFile); err != nil {
		return nil, fmt.Errorf("failed to read proving key: %w", err)
	}

	// Load or compile constraint system
	var ccs constraint.ConstraintSystem

	if r1csPath != "" {
		ccsFile, err := os.Open(r1csPath)
		if err == nil {
			defer ccsFile.Close()
			ccs = groth16.NewCS(ecc.BN254)
			if _, err := ccs.ReadFrom(ccsFile); err != nil {
				// Fall back to recompiling if read fails
				ccs = nil
			}
		}
	}

	// Compile if not loaded
	if ccs == nil {
		var voteCircuit circuit.VoteCircuit
		var err error
		ccs, err = frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &voteCircuit)
		if err != nil {
			return nil, fmt.Errorf("failed to compile circuit: %w", err)
		}
	}

	return &VoteProver{
		provingKey: pk,
		ccs:        ccs,
	}, nil
}

// NewVoteProverFromBytes creates a prover from in-memory key bytes
func NewVoteProverFromBytes(provingKeyBytes []byte, r1csBytes []byte) (*VoteProver, error) {
	pk := groth16.NewProvingKey(ecc.BN254)
	if _, err := pk.ReadFrom(bytes.NewReader(provingKeyBytes)); err != nil {
		return nil, fmt.Errorf("failed to read proving key: %w", err)
	}

	var ccs constraint.ConstraintSystem

	if len(r1csBytes) > 0 {
		ccs = groth16.NewCS(ecc.BN254)
		if _, err := ccs.ReadFrom(bytes.NewReader(r1csBytes)); err != nil {
			ccs = nil
		}
	}

	if ccs == nil {
		var voteCircuit circuit.VoteCircuit
		var err error
		ccs, err = frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &voteCircuit)
		if err != nil {
			return nil, fmt.Errorf("failed to compile circuit: %w", err)
		}
	}

	return &VoteProver{
		provingKey: pk,
		ccs:        ccs,
	}, nil
}

// GenerateProof creates a ZK proof for an anonymous vote
func (p *VoteProver) GenerateProof(input *VoteProofInput) (*VoteProofOutput, error) {
	// Validate inputs
	if err := validateInput(input); err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}

	// Verify the Merkle proof is valid (sanity check before expensive proving)
	publicKey := crypto.DerivePublicKey(input.SecretKey)
	expectedLeaf := crypto.ComputeLeaf(publicKey, input.VotingPower)

	proofCopy := &crypto.MerkleProof{
		Root:         input.MerkleRoot,
		Leaf:         expectedLeaf,
		LeafIndex:    input.MerkleProof.LeafIndex,
		PathElements: input.MerkleProof.PathElements,
		PathIndices:  input.MerkleProof.PathIndices,
	}

	if !proofCopy.Verify() {
		return nil, errors.New("merkle proof verification failed - check your credentials and voting power")
	}

	// Compute nullifier
	nullifier := crypto.ComputeNullifier(input.SecretKey, input.ProposalID)

	// Build circuit assignment
	assignment := &circuit.VoteCircuit{
		MerkleRoot:  crypto.BytesToFieldElement(input.MerkleRoot),
		Nullifier:   crypto.BytesToFieldElement(nullifier),
		ProposalID:  input.ProposalID,
		VoteOption:  uint64(input.VoteOption),
		VotingPower: input.VotingPower,
		SecretKey:   crypto.BytesToFieldElement(input.SecretKey),
	}

	// Set Merkle proof path
	for i := 0; i < circuit.TreeDepth; i++ {
		if i < len(input.MerkleProof.PathElements) {
			assignment.PathElements[i] = crypto.BytesToFieldElement(input.MerkleProof.PathElements[i])
			assignment.PathIndices[i] = input.MerkleProof.PathIndices[i]
		} else {
			assignment.PathElements[i] = 0
			assignment.PathIndices[i] = 0
		}
	}

	// Create witness
	witness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
	if err != nil {
		return nil, fmt.Errorf("failed to create witness: %w", err)
	}

	// Generate proof
	startTime := time.Now()
	proof, err := groth16.Prove(p.ccs, p.provingKey, witness)
	if err != nil {
		return nil, fmt.Errorf("proof generation failed: %w", err)
	}
	provingTime := time.Since(startTime)

	// Serialize proof
	var proofBuf bytes.Buffer
	if _, err := proof.WriteTo(&proofBuf); err != nil {
		return nil, fmt.Errorf("failed to serialize proof: %w", err)
	}

	return &VoteProofOutput{
		ProofBytes:  proofBuf.Bytes(),
		Nullifier:   nullifier,
		ProposalID:  input.ProposalID,
		VoteOption:  input.VoteOption,
		VotingPower: input.VotingPower,
		MerkleRoot:  input.MerkleRoot,
		ProvingTime: provingTime,
	}, nil
}

// validateInput checks that all required fields are present and valid
func validateInput(input *VoteProofInput) error {
	if len(input.SecretKey) == 0 {
		return errors.New("secret key is required")
	}
	if len(input.SecretKey) != 32 {
		return errors.New("secret key must be 32 bytes")
	}
	if input.VotingPower == 0 {
		return errors.New("voting power must be positive")
	}
	if input.VoteOption > 2 {
		return errors.New("vote option must be 0 (yes), 1 (no), or 2 (abstain)")
	}
	if len(input.MerkleRoot) == 0 {
		return errors.New("merkle root is required")
	}
	if input.MerkleProof == nil {
		return errors.New("merkle proof is required")
	}
	if len(input.MerkleProof.PathElements) != circuit.TreeDepth {
		return fmt.Errorf("merkle proof depth mismatch: got %d, expected %d",
			len(input.MerkleProof.PathElements), circuit.TreeDepth)
	}
	return nil
}

// ============================================================
// Key Management Helpers
// ============================================================

// GenerateSecretKey generates a new random secret key
func GenerateSecretKey() ([]byte, error) {
	secretKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, secretKey); err != nil {
		return nil, fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return secretKey, nil
}

// DeriveSecretKeyFromSeed derives a secret key from a seed phrase
// This allows voters to recover their voting identity
func DeriveSecretKeyFromSeed(seed string, salt string) []byte {
	combined := []byte(seed + salt)
	return crypto.HashToField(combined)
}

// GetPublicKey derives the public key from a secret key
func GetPublicKey(secretKey []byte) []byte {
	return crypto.DerivePublicKey(secretKey)
}

// PreviewVote returns what would be submitted without generating a proof
// Useful for verification before the expensive proof generation
func PreviewVote(input *VoteProofInput) (*VoteProofOutput, error) {
	if err := validateInput(input); err != nil {
		return nil, err
	}

	nullifier := crypto.ComputeNullifier(input.SecretKey, input.ProposalID)

	return &VoteProofOutput{
		ProofBytes:  nil, // No proof in preview
		Nullifier:   nullifier,
		ProposalID:  input.ProposalID,
		VoteOption:  input.VoteOption,
		VotingPower: input.VotingPower,
		MerkleRoot:  input.MerkleRoot,
	}, nil
}

// ============================================================
// Proof Verification (for testing)
// ============================================================

// VoteVerifier handles proof verification (typically done on-chain)
type VoteVerifier struct {
	verifyingKey groth16.VerifyingKey
}

// NewVoteVerifier creates a verifier from a key file
func NewVoteVerifier(verifyingKeyPath string) (*VoteVerifier, error) {
	vkFile, err := os.Open(verifyingKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open verifying key: %w", err)
	}
	defer vkFile.Close()

	vk := groth16.NewVerifyingKey(ecc.BN254)
	if _, err := vk.ReadFrom(vkFile); err != nil {
		return nil, fmt.Errorf("failed to read verifying key: %w", err)
	}

	return &VoteVerifier{verifyingKey: vk}, nil
}

// NewVoteVerifierFromBytes creates a verifier from in-memory key bytes
func NewVoteVerifierFromBytes(verifyingKeyBytes []byte) (*VoteVerifier, error) {
	vk := groth16.NewVerifyingKey(ecc.BN254)
	if _, err := vk.ReadFrom(bytes.NewReader(verifyingKeyBytes)); err != nil {
		return nil, fmt.Errorf("failed to read verifying key: %w", err)
	}

	return &VoteVerifier{verifyingKey: vk}, nil
}

// Verify checks if a vote proof is valid
func (v *VoteVerifier) Verify(output *VoteProofOutput) error {
	// Deserialize proof
	proof := groth16.NewProof(ecc.BN254)
	if _, err := proof.ReadFrom(bytes.NewReader(output.ProofBytes)); err != nil {
		return fmt.Errorf("failed to deserialize proof: %w", err)
	}

	// Build public witness
	publicAssignment := &circuit.VoteCircuit{
		MerkleRoot:  crypto.BytesToFieldElement(output.MerkleRoot),
		Nullifier:   crypto.BytesToFieldElement(output.Nullifier),
		ProposalID:  output.ProposalID,
		VoteOption:  uint64(output.VoteOption),
		VotingPower: output.VotingPower,
	}

	publicWitness, err := frontend.NewWitness(
		publicAssignment,
		ecc.BN254.ScalarField(),
		frontend.PublicOnly(),
	)
	if err != nil {
		return fmt.Errorf("failed to create public witness: %w", err)
	}

	// Verify
	if err := groth16.Verify(proof, v.verifyingKey, publicWitness); err != nil {
		return fmt.Errorf("proof verification failed: %w", err)
	}

	return nil
}
