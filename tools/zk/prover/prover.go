// Package prover provides client-side proof generation for anonymous shielded operations.
//
// This package is used by members to generate ZK proofs for their anonymous actions.
// The proofs can then be submitted to the blockchain via MsgShieldedExec.
//
// Usage:
//
//	prover, err := NewShieldProver("proving_key.bin", "circuit.r1cs")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	proof, err := prover.GenerateProof(input)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Submit proof.ProofBytes, proof.Nullifier, proof.RateLimitNullifier, etc. to chain
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

	"sparkdream/tools/crypto"
	"sparkdream/tools/zk/circuit"
)

// ShieldProver handles proof generation for anonymous shielded operations.
type ShieldProver struct {
	provingKey groth16.ProvingKey
	ccs        constraint.ConstraintSystem
}

// ShieldProofInput contains all inputs needed to generate a shielded proof.
type ShieldProofInput struct {
	// SecretKey is the member's secret key (32 bytes)
	SecretKey []byte

	// TrustLevel is the member's trust level (must match the Merkle tree leaf)
	TrustLevel uint64

	// MinTrustLevel is the minimum trust level required for this action
	MinTrustLevel uint64

	// Scope is the context binding for the action nullifier (epoch, postID, etc.)
	Scope uint64

	// RateLimitEpoch is the epoch for rate-limit nullifier binding
	RateLimitEpoch uint64

	// MerkleRoot is the root of the member trust tree
	MerkleRoot []byte

	// MerkleProof is the proof that the member is in the tree
	MerkleProof *crypto.MerkleProof
}

// ShieldProofOutput contains the generated proof and public inputs.
type ShieldProofOutput struct {
	// ProofBytes is the serialized ZK proof
	ProofBytes []byte

	// Nullifier prevents double actions per scope (submit this on-chain)
	Nullifier []byte

	// RateLimitNullifier enables per-identity rate limiting (submit this on-chain)
	RateLimitNullifier []byte

	// MerkleRoot should match the current trust tree root
	MerkleRoot []byte

	// MinTrustLevel is the minimum trust level proven
	MinTrustLevel uint64

	// Scope is the context binding used
	Scope uint64

	// RateLimitEpoch is the epoch the rate limit nullifier is bound to
	RateLimitEpoch uint64

	// ProvingTime is how long proof generation took
	ProvingTime time.Duration
}

// NewShieldProver creates a new prover from key files.
func NewShieldProver(provingKeyPath string, r1csPath string) (*ShieldProver, error) {
	pkFile, err := os.Open(provingKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open proving key: %w", err)
	}
	defer pkFile.Close()

	pk := groth16.NewProvingKey(ecc.BN254)
	if _, err := pk.ReadFrom(pkFile); err != nil {
		return nil, fmt.Errorf("failed to read proving key: %w", err)
	}

	var ccs constraint.ConstraintSystem

	if r1csPath != "" {
		ccsFile, err := os.Open(r1csPath)
		if err == nil {
			defer ccsFile.Close()
			ccs = groth16.NewCS(ecc.BN254)
			if _, err := ccs.ReadFrom(ccsFile); err != nil {
				ccs = nil
			}
		}
	}

	if ccs == nil {
		var shieldCircuit circuit.ShieldCircuit
		var err error
		ccs, err = frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &shieldCircuit)
		if err != nil {
			return nil, fmt.Errorf("failed to compile circuit: %w", err)
		}
	}

	return &ShieldProver{
		provingKey: pk,
		ccs:        ccs,
	}, nil
}

// NewShieldProverFromBytes creates a prover from in-memory key bytes.
func NewShieldProverFromBytes(provingKeyBytes []byte, r1csBytes []byte) (*ShieldProver, error) {
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
		var shieldCircuit circuit.ShieldCircuit
		var err error
		ccs, err = frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &shieldCircuit)
		if err != nil {
			return nil, fmt.Errorf("failed to compile circuit: %w", err)
		}
	}

	return &ShieldProver{
		provingKey: pk,
		ccs:        ccs,
	}, nil
}

// GenerateProof creates a ZK proof for an anonymous shielded operation.
func (p *ShieldProver) GenerateProof(input *ShieldProofInput) (*ShieldProofOutput, error) {
	if err := validateShieldInput(input); err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}

	// Verify the Merkle proof is valid (sanity check before expensive proving)
	publicKey := crypto.DerivePublicKey(input.SecretKey)
	expectedLeaf := crypto.ComputeLeaf(publicKey, input.TrustLevel)

	proofCopy := &crypto.MerkleProof{
		Root:         input.MerkleRoot,
		Leaf:         expectedLeaf,
		LeafIndex:    input.MerkleProof.LeafIndex,
		PathElements: input.MerkleProof.PathElements,
		PathIndices:  input.MerkleProof.PathIndices,
	}

	if !proofCopy.Verify() {
		return nil, errors.New("merkle proof verification failed - check your credentials and trust level")
	}

	// Compute nullifiers
	nullifier := crypto.ComputeNullifier(input.SecretKey, input.Scope)
	rateLimitNullifier := crypto.ComputeRateLimitNullifier(input.SecretKey, input.RateLimitEpoch)

	// Build circuit assignment
	assignment := &circuit.ShieldCircuit{
		MerkleRoot:         crypto.BytesToFieldElement(input.MerkleRoot),
		Nullifier:          crypto.BytesToFieldElement(nullifier),
		RateLimitNullifier: crypto.BytesToFieldElement(rateLimitNullifier),
		MinTrustLevel:      input.MinTrustLevel,
		Scope:              input.Scope,
		RateLimitEpoch:     input.RateLimitEpoch,
		SecretKey:          crypto.BytesToFieldElement(input.SecretKey),
		TrustLevel:         input.TrustLevel,
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

	return &ShieldProofOutput{
		ProofBytes:         proofBuf.Bytes(),
		Nullifier:          nullifier,
		RateLimitNullifier: rateLimitNullifier,
		MerkleRoot:         input.MerkleRoot,
		MinTrustLevel:      input.MinTrustLevel,
		Scope:              input.Scope,
		RateLimitEpoch:     input.RateLimitEpoch,
		ProvingTime:        provingTime,
	}, nil
}

// validateShieldInput checks that all required fields are present and valid.
func validateShieldInput(input *ShieldProofInput) error {
	if len(input.SecretKey) == 0 {
		return errors.New("secret key is required")
	}
	if len(input.SecretKey) != 32 {
		return errors.New("secret key must be 32 bytes")
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

// GenerateSecretKey generates a new random secret key.
func GenerateSecretKey() ([]byte, error) {
	secretKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, secretKey); err != nil {
		return nil, fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return secretKey, nil
}

// DeriveSecretKeyFromSeed derives a secret key from a seed phrase.
func DeriveSecretKeyFromSeed(seed string, salt string) []byte {
	combined := []byte(seed + salt)
	return crypto.HashToField(combined)
}

// GetPublicKey derives the public key from a secret key.
func GetPublicKey(secretKey []byte) []byte {
	return crypto.DerivePublicKey(secretKey)
}

// PreviewProof returns what would be submitted without generating an expensive proof.
func PreviewProof(input *ShieldProofInput) (*ShieldProofOutput, error) {
	if err := validateShieldInput(input); err != nil {
		return nil, err
	}

	nullifier := crypto.ComputeNullifier(input.SecretKey, input.Scope)
	rateLimitNullifier := crypto.ComputeRateLimitNullifier(input.SecretKey, input.RateLimitEpoch)

	return &ShieldProofOutput{
		ProofBytes:         nil, // No proof in preview
		Nullifier:          nullifier,
		RateLimitNullifier: rateLimitNullifier,
		MerkleRoot:         input.MerkleRoot,
		MinTrustLevel:      input.MinTrustLevel,
		Scope:              input.Scope,
		RateLimitEpoch:     input.RateLimitEpoch,
	}, nil
}

// ============================================================
// Proof Verification (for testing / off-chain validation)
// ============================================================

// ShieldVerifier handles proof verification.
type ShieldVerifier struct {
	verifyingKey groth16.VerifyingKey
}

// NewShieldVerifier creates a verifier from a key file.
func NewShieldVerifier(verifyingKeyPath string) (*ShieldVerifier, error) {
	vkFile, err := os.Open(verifyingKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open verifying key: %w", err)
	}
	defer vkFile.Close()

	vk := groth16.NewVerifyingKey(ecc.BN254)
	if _, err := vk.ReadFrom(vkFile); err != nil {
		return nil, fmt.Errorf("failed to read verifying key: %w", err)
	}

	return &ShieldVerifier{verifyingKey: vk}, nil
}

// NewShieldVerifierFromBytes creates a verifier from in-memory key bytes.
func NewShieldVerifierFromBytes(verifyingKeyBytes []byte) (*ShieldVerifier, error) {
	vk := groth16.NewVerifyingKey(ecc.BN254)
	if _, err := vk.ReadFrom(bytes.NewReader(verifyingKeyBytes)); err != nil {
		return nil, fmt.Errorf("failed to read verifying key: %w", err)
	}

	return &ShieldVerifier{verifyingKey: vk}, nil
}

// Verify checks if a shield proof is valid.
func (v *ShieldVerifier) Verify(output *ShieldProofOutput) error {
	proof := groth16.NewProof(ecc.BN254)
	if _, err := proof.ReadFrom(bytes.NewReader(output.ProofBytes)); err != nil {
		return fmt.Errorf("failed to deserialize proof: %w", err)
	}

	publicAssignment := &circuit.ShieldCircuit{
		MerkleRoot:         crypto.BytesToFieldElement(output.MerkleRoot),
		Nullifier:          crypto.BytesToFieldElement(output.Nullifier),
		RateLimitNullifier: crypto.BytesToFieldElement(output.RateLimitNullifier),
		MinTrustLevel:      output.MinTrustLevel,
		Scope:              output.Scope,
		RateLimitEpoch:     output.RateLimitEpoch,
	}

	publicWitness, err := frontend.NewWitness(
		publicAssignment,
		ecc.BN254.ScalarField(),
		frontend.PublicOnly(),
	)
	if err != nil {
		return fmt.Errorf("failed to create public witness: %w", err)
	}

	if err := groth16.Verify(proof, v.verifyingKey, publicWitness); err != nil {
		return fmt.Errorf("proof verification failed: %w", err)
	}

	return nil
}
