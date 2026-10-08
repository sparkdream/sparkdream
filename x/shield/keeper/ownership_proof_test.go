package keeper_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	any "github.com/cosmos/gogoproto/types/any"
	"github.com/stretchr/testify/require"

	zkcrypto "sparkdream/tools/crypto"
	"sparkdream/tools/zk/circuit"
	"sparkdream/tools/zk/prover"
	"sparkdream/x/shield/types"
)

// ownedModule is a ShieldAware module whose content has one anonymous owner.
type ownedModule struct {
	claim types.OwnershipClaim
}

func (m *ownedModule) IsShieldCompatible(context.Context, sdk.Msg) bool { return true }

func (m *ownedModule) ResolveOwnership(context.Context, sdk.Msg) (types.OwnershipClaim, error) {
	return m.claim, nil
}

func (m *ownedModule) AdvanceOwnership(context.Context, sdk.Msg) error {
	m.claim.Sequence++
	return nil
}

// zkSetup is a Groth16 setup of the shield circuit plus a prover for it.
type zkSetup struct {
	vk     groth16.VerifyingKey
	prover *prover.ShieldProver
}

func newZKSetup(t *testing.T) zkSetup {
	t.Helper()
	var c circuit.ShieldCircuit
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &c)
	require.NoError(t, err)
	pk, vk, err := groth16.Setup(ccs)
	require.NoError(t, err)

	var pkBuf, ccsBuf bytes.Buffer
	_, err = pk.WriteTo(&pkBuf)
	require.NoError(t, err)
	_, err = ccs.(constraint.ConstraintSystem).WriteTo(&ccsBuf)
	require.NoError(t, err)
	p, err := prover.NewShieldProverFromBytes(pkBuf.Bytes(), ccsBuf.Bytes())
	require.NoError(t, err)
	return zkSetup{vk: vk, prover: p}
}

// member is an anonymous identity in a one-leaf-per-member trust tree.
type member struct {
	sk   []byte
	path *zkcrypto.MerkleProof
}

func members(t *testing.T, secrets ...byte) []member {
	t.Helper()
	leaves := map[uint64][]byte{}
	for i, s := range secrets {
		leaves[uint64(i)] = zkcrypto.ComputeLeaf(zkcrypto.DerivePublicKey(zkcrypto.PadTo32([]byte{s})), 2)
	}
	out := make([]member, len(secrets))
	for i, s := range secrets {
		path, err := zkcrypto.SparseMerkleProof(leaves, circuit.TreeDepth, uint64(i))
		require.NoError(t, err)
		out[i] = member{sk: zkcrypto.PadTo32([]byte{s}), path: path}
	}
	return out
}

// exec proves as m over (domain, scope) bound to msgHash and wraps the proof in
// an immediate-mode MsgShieldedExec carrying inner.
func (z zkSetup) exec(t *testing.T, m member, domain uint32, scope uint64, inner *any.Any, msgHash []byte) *types.MsgShieldedExec {
	t.Helper()
	out, err := z.prover.GenerateProof(&prover.ShieldProofInput{
		SecretKey:      m.sk,
		TrustLevel:     2,
		MinTrustLevel:  1,
		Domain:         domain,
		Scope:          scope,
		RateLimitEpoch: 0,
		MessageHash:    msgHash,
		MerkleRoot:     m.path.Root,
		MerkleProof:    m.path,
	})
	require.NoError(t, err)
	return &types.MsgShieldedExec{
		ExecMode:           types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE,
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		MinTrustLevel:      1,
		InnerMessage:       inner,
		Nullifier:          out.Nullifier,
		RateLimitNullifier: out.RateLimitNullifier,
		MerkleRoot:         out.MerkleRoot,
		Proof:              out.ProofBytes,
	}
}

func innerAny(t *testing.T, msg sdk.Msg) *any.Any {
	t.Helper()
	a, err := codectypes.NewAnyWithValue(msg)
	require.NoError(t, err)
	return &any.Any{TypeUrl: a.TypeUrl, Value: a.Value}
}

// TestOwnershipModeWithRealProofs runs real Groth16 proofs through the keeper:
// the creator's proof over the owner scope reproduces the tag and verifies;
// another member, a stale sequence, or a plain message hash do not. It also
// checks that the proof's scope is domain-separated.
func TestOwnershipModeWithRealProofs(t *testing.T) {
	if testing.Short() {
		t.Skip("full Groth16 setup")
	}
	z := newZKSetup(t)
	f := initFixture(t)
	var vkBuf bytes.Buffer
	_, err := z.vk.WriteTo(&vkBuf)
	require.NoError(t, err)
	require.NoError(t, f.keeper.SetVerificationKey(f.ctx, types.VerificationKey{CircuitId: "shield_v1", VkBytes: vkBuf.Bytes()}))

	ms := members(t, 1, 2)
	creator, stranger := ms[0], ms[1]

	// The content was created by `creator` with a consumed proof over
	// (domain 21, scope 5); that nullifier is its owner tag.
	const ownerDomain, ownerScope = 21, 5
	tag := zkcrypto.ComputeScopedNullifier(creator.sk, ownerDomain, ownerScope)
	mod := &ownedModule{claim: types.OwnershipClaim{Domain: ownerDomain, Scope: ownerScope, Tag: tag}}
	f.keeper.RegisterShieldAwareModule("/sparkdream.shield.v1.", mod)

	inner := innerAny(t, &types.MsgTriggerDkg{Authority: "phoenix"})
	require.NoError(t, f.keeper.SetShieldedOp(f.ctx, types.ShieldedOpRegistration{
		MessageTypeUrl:  inner.TypeUrl,
		ProofDomain:     types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		MinTrustLevel:   1,
		NullifierDomain: 23,
		NullifierMode:   types.NullifierMode_NULLIFIER_MODE_OWNERSHIP,
		Active:          true,
		BatchMode:       types.ShieldBatchMode_SHIELD_BATCH_MODE_IMMEDIATE_ONLY,
	}))
	owned := func(seq uint64) []byte { return zkcrypto.ShieldOwnedMessageHash(inner.TypeUrl, inner.Value, seq) }

	// The creator, bound to the current sequence: accepted.
	good := z.exec(t, creator, ownerDomain, ownerScope, inner, owned(0))
	require.Equal(t, tag, good.Nullifier)
	require.NoError(t, f.keeper.PrecheckImmediate(f.ctx, good))

	// Bound to a plain message hash instead of the sequence: rejected.
	plain := z.exec(t, creator, ownerDomain, ownerScope, inner, zkcrypto.ShieldMessageHash(inner.TypeUrl, inner.Value))
	require.ErrorIs(t, f.keeper.PrecheckImmediate(f.ctx, plain), types.ErrInvalidProof)

	// Another member's proof yields another nullifier: not the owner.
	other := z.exec(t, stranger, ownerDomain, ownerScope, inner, owned(0))
	require.ErrorIs(t, f.keeper.PrecheckImmediate(f.ctx, other), types.ErrOwnershipMismatch)
	// Claiming the tag with their own proof doesn't verify.
	other.Nullifier = tag
	require.ErrorIs(t, f.keeper.PrecheckImmediate(f.ctx, other), types.ErrInvalidProof)

	// Once the sequence advances, the accepted proof can't be replayed.
	require.NoError(t, mod.AdvanceOwnership(f.ctx, nil))
	require.ErrorIs(t, f.keeper.PrecheckImmediate(f.ctx, good), types.ErrInvalidProof)
	require.NoError(t, f.keeper.PrecheckImmediate(f.ctx, z.exec(t, creator, ownerDomain, ownerScope, inner, owned(1))))

	// In CheckTx the precheck advances the sequence, so a rewrapped copy of a
	// pending exec is refused from the mempool.
	pending := z.exec(t, creator, ownerDomain, ownerScope, inner, owned(1))
	checkCtx := f.ctx.WithIsCheckTx(true)
	require.NoError(t, f.keeper.PrecheckImmediate(checkCtx, pending))
	require.ErrorIs(t, f.keeper.PrecheckImmediate(checkCtx, pending), types.ErrInvalidProof)
}

// TestScopeIsDomainSeparated: a proof made for one nullifier domain doesn't
// verify for another domain with the same raw scope, so one member's
// nullifiers in different operations never match.
func TestScopeIsDomainSeparated(t *testing.T) {
	if testing.Short() {
		t.Skip("full Groth16 setup")
	}
	z := newZKSetup(t)
	f := initFixture(t)
	var vkBuf bytes.Buffer
	_, err := z.vk.WriteTo(&vkBuf)
	require.NoError(t, err)
	require.NoError(t, f.keeper.SetVerificationKey(f.ctx, types.VerificationKey{CircuitId: "shield_v1", VkBytes: vkBuf.Bytes()}))
	m := members(t, 3)[0]

	inner := innerAny(t, &types.MsgTriggerDkg{Authority: "aurora"})
	f.keeper.RegisterShieldAwareModule("/sparkdream.shield.v1.", &ownedModule{})
	require.NoError(t, f.keeper.SetShieldedOp(f.ctx, types.ShieldedOpRegistration{
		MessageTypeUrl:     inner.TypeUrl,
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		MinTrustLevel:      1,
		NullifierDomain:    11,
		NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_GLOBAL,
		Active:             true,
		BatchMode:          types.ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
	}))
	msgHash := zkcrypto.ShieldMessageHash(inner.TypeUrl, inner.Value)

	wrongDomain := z.exec(t, m, 1, 0, inner, msgHash)
	require.ErrorIs(t, f.keeper.PrecheckImmediate(f.ctx, wrongDomain), types.ErrInvalidProof)

	right := z.exec(t, m, 11, 0, inner, msgHash)
	require.NoError(t, f.keeper.PrecheckImmediate(f.ctx, right))
	require.NotEqual(t, wrongDomain.Nullifier, right.Nullifier, "same raw scope, different domains")
}
