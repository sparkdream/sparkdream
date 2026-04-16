package keeper

import (
	"bytes"
	"context"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"

	"sparkdream/tools/zk/circuit"
	"sparkdream/x/shield/types"
)

// verifyProof verifies a ZK proof (Groth16 over BN254) for an immediate mode shielded execution.
// The unified ShieldCircuit is used for all proof domains.
func (k Keeper) verifyProof(ctx context.Context, msg *types.MsgShieldedExec, scope uint64) error {
	// 1. Look up verification key — unified circuit for all domains.
	// When no VK is stored (test mode / early startup), skip ALL verification
	// including merkle root validation. Without proof verification the merkle
	// root is just a public input that cannot be checked, so validating it
	// would only block test-mode usage for no security benefit.
	const circuitID = "shield_v1"
	storedVK, found := k.GetVerificationKeyVal(ctx, circuitID)
	if !found || len(storedVK.VkBytes) == 0 {
		if requireVerificationKey() {
			return types.ErrNoVerificationKey
		}
		// Test mode: no VK registered, skip all proof verification.
		// This allows E2E tests to exercise MsgShieldedExec with dummy proofs.
		return nil
	}

	// 2. Validate merkle root is current or previous for the given proof domain
	if err := k.validateMerkleRoot(ctx, msg.MerkleRoot, msg.ProofDomain); err != nil {
		return err
	}

	// 3. Deserialize the Groth16 verification key
	vk := groth16.NewVerifyingKey(ecc.BN254)
	if _, err := vk.ReadFrom(bytes.NewReader(storedVK.VkBytes)); err != nil {
		return types.ErrInvalidProof
	}

	// 4. Deserialize the proof
	if len(msg.Proof) == 0 {
		return types.ErrInvalidProof
	}
	proof := groth16.NewProof(ecc.BN254)
	if _, err := proof.ReadFrom(bytes.NewReader(msg.Proof)); err != nil {
		return types.ErrInvalidProof
	}

	// 5. Construct public witness and verify
	epoch := k.GetCurrentEpoch(ctx)

	return k.verifyShieldProof(vk, proof, msg, scope, epoch)
}

// verifyShieldProof verifies a Groth16 proof against the unified ShieldCircuit.
//
// Public inputs: MerkleRoot, Nullifier, RateLimitNullifier, MinTrustLevel, Scope, RateLimitEpoch
//
// The scope parameter is the resolved nullifier scope (epoch, message field value, or 0 for global).
// The epoch parameter is the current shield epoch (used for rate-limit nullifier binding).
func (k Keeper) verifyShieldProof(
	vk groth16.VerifyingKey,
	proof groth16.Proof,
	msg *types.MsgShieldedExec,
	scope uint64,
	epoch uint64,
) error {
	assignment := &circuit.ShieldCircuit{
		MerkleRoot:         new(big.Int).SetBytes(msg.MerkleRoot),
		Nullifier:          new(big.Int).SetBytes(msg.Nullifier),
		RateLimitNullifier: new(big.Int).SetBytes(msg.RateLimitNullifier),
		MinTrustLevel:      msg.MinTrustLevel,
		Scope:              scope,
		RateLimitEpoch:     epoch,
	}

	publicWitness, err := frontend.NewWitness(
		assignment,
		ecc.BN254.ScalarField(),
		frontend.PublicOnly(),
	)
	if err != nil {
		return types.ErrInvalidProof
	}

	if err := groth16.Verify(proof, vk, publicWitness); err != nil {
		return types.ErrInvalidProof
	}
	return nil
}

// validateMerkleRoot checks that the provided merkle root is current or previous.
func (k Keeper) validateMerkleRoot(ctx context.Context, merkleRoot []byte, proofDomain types.ProofDomain) error {
	if k.late.repKeeper == nil {
		// RepKeeper not wired — skip validation (testing/early startup)
		return nil
	}

	switch proofDomain {
	case types.ProofDomain_PROOF_DOMAIN_TRUST_TREE:
		current, err := k.late.repKeeper.GetTrustTreeRoot(ctx)
		if err != nil {
			return types.ErrInvalidMerkleRoot
		}
		if bytesEqual(merkleRoot, current) {
			return nil
		}
		previous, err := k.late.repKeeper.GetPreviousTrustTreeRoot(ctx)
		if err != nil {
			return types.ErrInvalidMerkleRoot
		}
		if bytesEqual(merkleRoot, previous) {
			return nil
		}
		return types.ErrInvalidMerkleRoot

	default:
		return types.ErrInvalidProofDomain
	}
}

// GetVerificationKeyVal returns a verification key by circuit ID.
func (k Keeper) GetVerificationKeyVal(ctx context.Context, circuitID string) (types.VerificationKey, bool) {
	vk, err := k.VerificationKeys.Get(ctx, circuitID)
	if err != nil {
		return types.VerificationKey{}, false
	}
	return vk, true
}

// SetVerificationKey stores a verification key.
func (k Keeper) SetVerificationKey(ctx context.Context, vk types.VerificationKey) error {
	return k.VerificationKeys.Set(ctx, vk.CircuitId, vk)
}

// bytesEqual compares two byte slices.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
