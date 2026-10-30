package keeper_test

import (
	"bytes"
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	any "github.com/cosmos/gogoproto/types/any"
	"github.com/stretchr/testify/require"

	zkcrypto "sparkdream/tools/crypto"
	"sparkdream/tools/zk/circuit"
	"sparkdream/tools/zk/prover"
	"sparkdream/x/shield/types"
)

// groupModule is a ShieldAware module that keeps its own voter tree (as
// x/commons secret ballots do) and names its root for every message.
type groupModule struct{ root []byte }

func (m *groupModule) IsShieldCompatible(context.Context, sdk.Msg) bool { return true }
func (m *groupModule) ShieldMerkleRoot(context.Context, sdk.Msg) ([]byte, error) {
	return m.root, nil
}

// groupExec proves membership of a group tree (leaves MiMC(pk, 0)) with the
// unified shield circuit and wraps it as a GROUP_TREE immediate exec.
func groupExec(t *testing.T, z zkSetup, sk []byte, path *zkcrypto.MerkleProof, domain uint32, inner *any.Any) *types.MsgShieldedExec {
	t.Helper()
	out, err := z.prover.GenerateProof(&prover.ShieldProofInput{
		SecretKey:      sk,
		TrustLevel:     0,
		MinTrustLevel:  0,
		Domain:         domain,
		Scope:          0,
		RateLimitEpoch: 0,
		MessageHash:    zkcrypto.ShieldMessageHash(inner.TypeUrl, inner.Value),
		MerkleRoot:     path.Root,
		MerkleProof:    path,
	})
	require.NoError(t, err)
	return &types.MsgShieldedExec{
		ExecMode:           types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE,
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_GROUP_TREE,
		InnerMessage:       inner,
		Nullifier:          out.Nullifier,
		RateLimitNullifier: out.RateLimitNullifier,
		MerkleRoot:         out.MerkleRoot,
		Proof:              out.ProofBytes,
	}
}

// TestGroupTreeProofs: the existing circuit proves membership of a module's
// own voter tree. A GROUP_TREE exec verifies only against the root the
// inner message's module names; another root, the trust-tree domain, a
// module that keeps no tree, and a batch-shaped exec (no inner message to
// resolve the root from) are refused.
func TestGroupTreeProofs(t *testing.T) {
	if testing.Short() {
		t.Skip("full Groth16 setup")
	}
	z := newZKSetup(t)
	f := initFixture(t)
	var vkBuf bytes.Buffer
	_, err := z.vk.WriteTo(&vkBuf)
	require.NoError(t, err)
	require.NoError(t, f.keeper.SetVerificationKey(f.ctx, types.VerificationKey{CircuitId: "shield_v1", VkBytes: vkBuf.Bytes()}))

	secrets := [][]byte{zkcrypto.PadTo32([]byte{7}), zkcrypto.PadTo32([]byte{8}), zkcrypto.PadTo32([]byte{9})}
	leaves := map[uint64][]byte{}
	for i, sk := range secrets {
		leaves[uint64(i)] = zkcrypto.ComputeLeaf(zkcrypto.DerivePublicKey(sk), 0)
	}
	paths := make([]*zkcrypto.MerkleProof, len(secrets))
	for i := range secrets {
		paths[i], err = zkcrypto.SparseMerkleProof(leaves, circuit.TreeDepth, uint64(i))
		require.NoError(t, err)
	}

	mod := &groupModule{root: paths[0].Root}
	f.keeper.RegisterShieldAwareModule("/sparkdream.shield.v1.", mod)
	inner := innerAny(t, &types.MsgTriggerDkg{Authority: "aurora"})
	const domain = 33
	require.NoError(t, f.keeper.SetShieldedOp(f.ctx, types.ShieldedOpRegistration{
		MessageTypeUrl:     inner.TypeUrl,
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_GROUP_TREE,
		NullifierDomain:    domain,
		NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_GLOBAL,
		Active:             true,
		BatchMode:          types.ShieldBatchMode_SHIELD_BATCH_MODE_IMMEDIATE_ONLY,
	}))

	// A member of the module's tree: accepted.
	require.NoError(t, f.keeper.PrecheckImmediateErr(f.ctx, groupExec(t, z, secrets[0], paths[0], domain, inner)))

	// The module names another root (the ballot changed or is over).
	mod.root = make([]byte, 32)
	require.ErrorIs(t, f.keeper.PrecheckImmediateErr(f.ctx, groupExec(t, z, secrets[1], paths[1], domain, inner)), types.ErrInvalidMerkleRoot)
	mod.root = paths[0].Root

	// The same proof presented as a trust-tree proof: wrong domain.
	asTrust := groupExec(t, z, secrets[1], paths[1], domain, inner)
	asTrust.ProofDomain = types.ProofDomain_PROOF_DOMAIN_TRUST_TREE
	require.ErrorIs(t, f.keeper.PrecheckImmediateErr(f.ctx, asTrust), types.ErrProofDomainMismatch)

	// A module that keeps no tree cannot be proven against.
	f.keeper.RegisterShieldAwareModule("/sparkdream.shield.v1.", &ownedModule{})
	require.ErrorIs(t, f.keeper.PrecheckImmediateErr(f.ctx, groupExec(t, z, secrets[2], paths[2], domain, inner)), types.ErrInvalidProofDomain)

	// A batch submission carries no inner message, so its group root cannot
	// be resolved.
	require.ErrorIs(t, f.keeper.ValidateMerkleRoot(f.ctx, &types.MsgShieldedExec{
		ProofDomain: types.ProofDomain_PROOF_DOMAIN_GROUP_TREE, MerkleRoot: paths[0].Root,
	}), types.ErrInvalidProofDomain)
}
