package keeper

import (
	"bytes"
	"context"
	"math/big"

	errorsmod "cosmossdk.io/errors"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	zkcrypto "sparkdream/tools/crypto"
	"sparkdream/tools/zk/circuit"
	"sparkdream/x/shield/types"
)

// proofBinding is what the chain binds a proof's Scope and MessageHash public
// inputs to.
type proofBinding struct {
	// scope is the Scope public input: crypto.ScopeElement(domain, rawScope).
	scope []byte
	// messageHash is the MessageHash public input: crypto.ShieldMessageHash,
	// or crypto.ShieldOwnedMessageHash for an ownership-mode exec.
	messageHash []byte
}

// consumeBinding is the binding for an ordinary (nullifier-consuming) exec.
func consumeBinding(domain uint32, rawScope uint64, msg *types.MsgShieldedExec) proofBinding {
	return proofBinding{scope: zkcrypto.ScopeElement(domain, rawScope), messageHash: messageHash(msg)}
}

// ownedBinding is the binding for an ownership-mode exec: the owner's scope and
// a message hash that also covers the owner's current sequence.
func ownedBinding(claim types.OwnershipClaim, msg *types.MsgShieldedExec) proofBinding {
	return proofBinding{
		scope:       zkcrypto.ScopeElement(claim.Domain, claim.Scope),
		messageHash: zkcrypto.ShieldOwnedMessageHash(msg.InnerMessage.TypeUrl, msg.InnerMessage.Value, claim.Sequence),
	}
}

// verifyProof verifies a ZK proof (Groth16 over BN254) for an immediate mode
// shielded execution: the merkle root is checked against the live tree and the
// rate-limit epoch is the current shield epoch. The unified ShieldCircuit is
// used for all proof domains.
func (k Keeper) verifyProof(ctx context.Context, msg *types.MsgShieldedExec, binding proofBinding) error {
	return k.verifyProofAtEpoch(ctx, msg, binding, k.GetCurrentEpoch(ctx), true)
}

// verifyProofAtEpoch is the shared verifier. `epoch` is the shield epoch the
// prover bound into RateLimitEpoch; for an immediate exec that is the current
// epoch, for an encrypted-batch op it is the epoch the op was submitted in
// (the EndBlocker advances the epoch before it runs the batch, so the current
// epoch is always at least one past the prover's). `checkRoot` re-validates
// the merkle root against the live tree; batch ops pass false because the
// root was validated at submission and the tree may have rotated since
// ("valid when submitted", spec §Merkle Root Validation).
func (k Keeper) verifyProofAtEpoch(ctx context.Context, msg *types.MsgShieldedExec, binding proofBinding, epoch uint64, checkRoot bool) error {
	// 1. Look up verification key — unified circuit for all domains.
	// When no VK is stored (test mode / early startup), skip ALL verification
	// including merkle root validation. Without proof verification the merkle
	// root is just a public input that cannot be checked, so validating it
	// would only block test-mode usage for no security benefit.
	storedVK, found := k.GetVerificationKeyVal(ctx, types.ShieldCircuitID)
	if !found || len(storedVK.VkBytes) == 0 {
		if requireVerificationKey() {
			return types.ErrNoVerificationKey
		}
		// Test mode: no VK registered, skip all proof verification.
		// This allows E2E tests to exercise MsgShieldedExec with dummy proofs.
		return nil
	}

	// 2. Validate merkle root is current or previous for the given proof domain
	if checkRoot {
		if err := k.validateMerkleRoot(ctx, msg); err != nil {
			return err
		}
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
	return k.verifyShieldProof(vk, proof, msg, binding, rateLimitEpochInput(msg.ProofDomain, binding.scope, epoch))
}

// rateLimitEpochInput is the RateLimitEpoch public input for an exec in the
// given shield epoch. A PROOF_DOMAIN_GROUP_TREE exec (a secret-ballot vote)
// binds the epoch to its scope (crypto.GroupTreeRateLimitEpochForScope), so
// its rate-limit nullifier is unique to the member and the ballot and links
// neither two votes nor a vote and a trust-tree action in the same epoch
// (SHD-101). Every other domain uses the plain epoch: its rate-limit
// nullifier is the member's per-epoch quota key.
func rateLimitEpochInput(domain types.ProofDomain, scope []byte, epoch uint64) *big.Int {
	if domain == types.ProofDomain_PROOF_DOMAIN_GROUP_TREE {
		return new(big.Int).SetBytes(zkcrypto.GroupTreeRateLimitEpochForScope(scope, epoch))
	}
	return new(big.Int).SetUint64(epoch)
}

// verifyShieldProof verifies a Groth16 proof against the unified ShieldCircuit.
//
// Public inputs: MerkleRoot, Nullifier, RateLimitNullifier, MinTrustLevel, Scope, RateLimitEpoch, MessageHash
//
// Scope and MessageHash come from the binding: the scope is the domain-separated
// scope element (never the raw scope: the circuit hashes only secret key and
// scope, so a raw scope shared by two domains would give one member the same
// public nullifier in both). rateLimitEpoch is the RateLimitEpoch public input
// (rateLimitEpochInput).
func (k Keeper) verifyShieldProof(
	vk groth16.VerifyingKey,
	proof groth16.Proof,
	msg *types.MsgShieldedExec,
	binding proofBinding,
	rateLimitEpoch *big.Int,
) error {
	assignment := &circuit.ShieldCircuit{
		MerkleRoot:         new(big.Int).SetBytes(msg.MerkleRoot),
		Nullifier:          new(big.Int).SetBytes(msg.Nullifier),
		RateLimitNullifier: new(big.Int).SetBytes(msg.RateLimitNullifier),
		MinTrustLevel:      msg.MinTrustLevel,
		Scope:              new(big.Int).SetBytes(binding.scope),
		RateLimitEpoch:     rateLimitEpoch,
		MessageHash:        new(big.Int).SetBytes(binding.messageHash),
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

// validateMerkleRoot checks the exec's merkle root against its proof domain:
// the current or previous trust-tree root, or, for a group tree, the root
// the inner message's module names for it.
func (k Keeper) validateMerkleRoot(ctx context.Context, msg *types.MsgShieldedExec) error {
	merkleRoot, proofDomain := msg.MerkleRoot, msg.ProofDomain
	if proofDomain == types.ProofDomain_PROOF_DOMAIN_GROUP_TREE {
		return k.validateGroupTreeRoot(ctx, msg)
	}
	if k.late.repKeeper == nil {
		// RepKeeper not wired: a network build fails closed (accepting any
		// root would accept proofs against a made-up tree); the test build
		// skips the check so keeper tests need no rep keeper.
		if requireVerificationKey() {
			return errorsmod.Wrap(types.ErrInvalidMerkleRoot, "rep keeper not wired")
		}
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

// validateGroupTreeRoot checks a PROOF_DOMAIN_GROUP_TREE exec's root against
// the one the inner message's module names (ShieldRootResolver). The inner
// message must be in the clear: a batch submission is checked before its
// payload is decrypted, so a group-tree proof cannot ride in one.
func (k Keeper) validateGroupTreeRoot(ctx context.Context, msg *types.MsgShieldedExec) error {
	if msg.InnerMessage == nil || msg.InnerMessage.TypeUrl == "" {
		return errorsmod.Wrap(types.ErrInvalidProofDomain, "group-tree proofs are immediate-mode only")
	}
	var innerMsg sdk.Msg
	if err := k.cdc.UnpackAny(&codectypes.Any{TypeUrl: msg.InnerMessage.TypeUrl, Value: msg.InnerMessage.Value}, &innerMsg); err != nil {
		return errorsmod.Wrap(types.ErrInvalidInnerMessage, err.Error())
	}
	sa, found := k.getShieldAware(msg.InnerMessage.TypeUrl)
	if !found {
		return types.ErrInvalidMerkleRoot
	}
	resolver, ok := sa.(types.ShieldRootResolver)
	if !ok {
		return errorsmod.Wrapf(types.ErrInvalidProofDomain, "%s keeps no group tree", msg.InnerMessage.TypeUrl)
	}
	root, err := resolver.ShieldMerkleRoot(ctx, innerMsg)
	if err != nil {
		return errorsmod.Wrap(types.ErrInvalidMerkleRoot, err.Error())
	}
	if len(root) == 0 || !bytesEqual(msg.MerkleRoot, root) {
		return types.ErrInvalidMerkleRoot
	}
	return nil
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

// messageHash is the hash a proof binds its inner message to; see
// crypto.ShieldMessageHash. It covers the inner Any exactly as submitted.
func messageHash(msg *types.MsgShieldedExec) []byte {
	if msg.InnerMessage == nil {
		return nil
	}
	return zkcrypto.ShieldMessageHash(msg.InnerMessage.TypeUrl, msg.InnerMessage.Value)
}
