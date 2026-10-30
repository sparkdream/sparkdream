package keeper_test

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/stretchr/testify/require"

	zkcrypto "sparkdream/tools/crypto"
	"sparkdream/x/shield/types"
)

// plusModulus returns b + r (the BN254 scalar field modulus) as 32 big-endian
// bytes, or nil when the sum does not fit in 32 bytes.
func plusModulus(b []byte) []byte {
	sum := new(big.Int).Add(new(big.Int).SetBytes(b), fr.Modulus())
	if sum.BitLen() > 256 {
		return nil
	}
	return sum.FillBytes(make([]byte, 32))
}

// TestNonCanonicalPublicInputsRejected (SHD-01): the Groth16 verifier reduces
// public inputs mod r, so a real proof also verifies with nullifier + r or
// rate-limit nullifier + r. Those are distinct store keys, so accepting them
// would let one proof act several times in one scope, bypass the identity rate
// limit, and let anyone replay a landed exec under a fresh encoding. Every
// validate path must refuse the non-canonical encoding.
func TestNonCanonicalPublicInputsRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("full Groth16 setup")
	}
	z := newZKSetup(t)
	f := initFixture(t)
	var vkBuf bytes.Buffer
	_, err := z.vk.WriteTo(&vkBuf)
	require.NoError(t, err)
	require.NoError(t, f.keeper.SetVerificationKey(f.ctx, types.VerificationKey{CircuitId: types.ShieldCircuitID, VkBytes: vkBuf.Bytes()}))

	const domain, scope = 23, 0
	inner := innerAny(t, &types.MsgTriggerDkg{Authority: "phoenix"})
	require.NoError(t, f.keeper.SetShieldedOp(f.ctx, types.ShieldedOpRegistration{
		MessageTypeUrl:     inner.TypeUrl,
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		MinTrustLevel:      1,
		NullifierDomain:    domain,
		NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_GLOBAL,
		Active:             true,
		BatchMode:          types.ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
	}))

	m := members(t, 7)[0]
	good := z.exec(t, m, domain, scope, inner, zkcrypto.ShieldMessageHash(inner.TypeUrl, inner.Value))
	require.True(t, zkcrypto.IsCanonicalFieldElement(good.Nullifier))

	// n + r must fit in 32 bytes for the attack (true for ~5 in 6 values); the
	// proof's nullifier is deterministic for this member and scope.
	nPlusR := plusModulus(good.Nullifier)
	rlPlusR := plusModulus(good.RateLimitNullifier)
	require.NotNil(t, nPlusR, "pick another member secret: n + r overflows 32 bytes")
	require.NotNil(t, rlPlusR, "pick another member secret: rl + r overflows 32 bytes")
	require.False(t, zkcrypto.IsCanonicalFieldElement(nPlusR))

	withNullifier := func(n, rl, root []byte) *types.MsgShieldedExec {
		c := *good
		c.Nullifier, c.RateLimitNullifier, c.MerkleRoot = n, rl, root
		return &c
	}
	aliasN := withNullifier(nPlusR, good.RateLimitNullifier, good.MerkleRoot)
	aliasRL := withNullifier(good.Nullifier, rlPlusR, good.MerkleRoot)

	// The bug class: the bare verifier accepts both aliases.
	require.NoError(t, f.keeper.VerifyConsumeProof(f.ctx, good, domain, scope, 0))
	require.NoError(t, f.keeper.VerifyConsumeProof(f.ctx, aliasN, domain, scope, 0), "verifier reduces mod r")
	require.NoError(t, f.keeper.VerifyConsumeProof(f.ctx, aliasRL, domain, scope, 0), "verifier reduces mod r")

	// Immediate mode: the aliases are refused before any state check.
	require.ErrorIs(t, f.keeper.PrecheckImmediateErr(f.ctx, aliasN), types.ErrNonCanonicalPublicInput)
	require.ErrorIs(t, f.keeper.PrecheckImmediateErr(f.ctx, aliasRL), types.ErrNonCanonicalPublicInput)
	if rootPlusR := plusModulus(good.MerkleRoot); rootPlusR != nil {
		require.ErrorIs(t, f.keeper.PrecheckImmediateErr(f.ctx, withNullifier(good.Nullifier, good.RateLimitNullifier, rootPlusR)), types.ErrNonCanonicalPublicInput)
	}
	require.NoError(t, f.keeper.PrecheckImmediateErr(f.ctx, good))

	// Once the canonical nullifier is used, no alias re-opens the scope.
	require.NoError(t, f.keeper.RecordNullifier(f.ctx, domain, scope, hex.EncodeToString(good.Nullifier), 1))
	require.ErrorIs(t, f.keeper.PrecheckImmediateErr(f.ctx, good), types.ErrNullifierUsed)
	require.ErrorIs(t, f.keeper.PrecheckImmediateErr(f.ctx, aliasN), types.ErrNonCanonicalPublicInput)

	// Encrypted-batch submission: same rule for the cleartext nullifiers.
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 0, EpochStartHeight: 1}))
	batch := &types.MsgShieldedExec{
		ExecMode:           types.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH,
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		EncryptedPayload:   []byte("ciphertext"),
		Nullifier:          nPlusR,
		RateLimitNullifier: good.RateLimitNullifier,
		MerkleRoot:         good.MerkleRoot,
		Proof:              good.Proof,
	}
	require.ErrorIs(t, f.keeper.PrecheckExecErr(f.ctx, batch), types.ErrNonCanonicalPublicInput)
	batch.Nullifier, batch.RateLimitNullifier = good.Nullifier, rlPlusR
	require.ErrorIs(t, f.keeper.PrecheckExecErr(f.ctx, batch), types.ErrNonCanonicalPublicInput)
}

// TestValidateCanonicalPublicInputsBoundary pins the boundary: r - 1 is the
// largest accepted value, r and above are refused, shorter encodings (and an
// absent root) read as small values and pass.
func TestValidateCanonicalPublicInputsBoundary(t *testing.T) {
	r := fr.Modulus()
	rMinus1 := new(big.Int).Sub(r, big.NewInt(1)).FillBytes(make([]byte, 32))
	rBytes := r.FillBytes(make([]byte, 32))
	ok := make([]byte, 32)

	require.NoError(t, types.ValidateCanonicalPublicInputs(rMinus1, rMinus1, rMinus1))
	require.NoError(t, types.ValidateCanonicalPublicInputs(ok, ok, nil))
	require.ErrorIs(t, types.ValidateCanonicalPublicInputs(rBytes, ok, ok), types.ErrNonCanonicalPublicInput)
	require.ErrorIs(t, types.ValidateCanonicalPublicInputs(ok, rBytes, ok), types.ErrNonCanonicalPublicInput)
	require.ErrorIs(t, types.ValidateCanonicalPublicInputs(ok, ok, rBytes), types.ErrNonCanonicalPublicInput)
	require.ErrorIs(t, types.ValidateCanonicalPublicInputs(bytes.Repeat([]byte{0xff}, 32), ok, ok), types.ErrNonCanonicalPublicInput)
}
