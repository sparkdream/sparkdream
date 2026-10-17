package keeper_test

import (
	"bytes"
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4/util/random"

	zkcrypto "sparkdream/tools/crypto"
	"sparkdream/tools/tle"
	"sparkdream/tools/zk/prover"
	"sparkdream/x/shield/keeper"
	"sparkdream/x/shield/types"
)

// TestEncryptedBatchVerifiesAgainstSubmissionEpoch runs a real Groth16 proof
// through the encrypted-batch path. The prover binds RateLimitEpoch to the
// epoch the op is submitted in; the EndBlocker advances the epoch before it
// runs the batch, so verification must use the stored submission epoch, not
// the current one. A proof bound to the wrong epoch is still rejected.
func TestEncryptedBatchVerifiesAgainstSubmissionEpoch(t *testing.T) {
	if testing.Short() {
		t.Skip("full Groth16 setup")
	}
	z := newZKSetup(t)
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	var vkBuf bytes.Buffer
	_, err := z.vk.WriteTo(&vkBuf)
	require.NoError(t, err)
	require.NoError(t, f.keeper.SetVerificationKey(f.ctx, types.VerificationKey{CircuitId: "shield_v1", VkBytes: vkBuf.Bytes()}))

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	params.MinBatchSize = 1
	params.ShieldEpochInterval = 10
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	const submitEpoch = uint64(7)
	startHeight := f.ctx.BlockHeight()
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{
		CurrentEpoch: submitEpoch, EpochStartHeight: startHeight,
	}))

	// A GLOBAL-scope registration that allows batch execution.
	inner := innerAny(t, &types.MsgTriggerDkg{Authority: "aurora"})
	f.keeper.RegisterShieldAwareModule("/sparkdream.shield.v1.", &ownedModule{})
	const domain = uint32(11)
	require.NoError(t, f.keeper.SetShieldedOp(f.ctx, types.ShieldedOpRegistration{
		MessageTypeUrl:     inner.TypeUrl,
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		MinTrustLevel:      1,
		NullifierDomain:    domain,
		NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_GLOBAL,
		Active:             true,
		BatchMode:          types.ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
	}))
	msgHash := zkcrypto.ShieldMessageHash(inner.TypeUrl, inner.Value)

	// Key material as a real client sees it: only the G2 master public key.
	// The chain later holds the epoch key s*H1(epoch) (here computed from s;
	// on a live chain reconstructed from validator shares).
	master := tle.Suite.G1().Scalar().Pick(random.New())
	mpkG2 := tle.MasterPublicKeyG2(master)
	keyBytes := tle.EpochDecryptionKey(master, submitEpoch)

	ms2 := members(t, 4, 5)
	submitter, err := f.addressCodec.BytesToString([]byte("batch_submitter_____"))
	require.NoError(t, err)

	// submit proves as m with RateLimitEpoch=epoch and queues the encrypted op.
	submit := func(m member, epoch uint64) *prover.ShieldProofOutput {
		out, err := z.prover.GenerateProof(&prover.ShieldProofInput{
			SecretKey:      m.sk,
			TrustLevel:     2,
			MinTrustLevel:  1,
			Domain:         domain,
			Scope:          0,
			RateLimitEpoch: epoch,
			MessageHash:    msgHash,
			MerkleRoot:     m.path.Root,
			MerkleProof:    m.path,
		})
		require.NoError(t, err)
		// The web client's hand-rolled fragment must decode as the SDK type.
		plaintext, err := tle.EncodeExecFragment(inner.TypeUrl, inner.Value, out.ProofBytes, out.Nullifier)
		require.NoError(t, err)
		sdkPlain, err := (&types.MsgShieldedExec{InnerMessage: inner, Proof: out.ProofBytes, Nullifier: out.Nullifier}).Marshal()
		require.NoError(t, err)
		require.Equal(t, sdkPlain, plaintext, "fragment encoding drifted from the SDK codec")
		payload, err := tle.Encrypt(mpkG2, submitEpoch, plaintext)
		require.NoError(t, err)
		// The submission proof: same member, current epoch, bound to the
		// ciphertext (domain/scope/message as the web client derives them).
		sub, err := z.prover.GenerateProof(&prover.ShieldProofInput{
			SecretKey:      m.sk,
			TrustLevel:     2,
			MinTrustLevel:  1,
			Domain:         tle.BatchSubmissionDomain,
			Scope:          tle.BatchSubmissionScope(payload),
			RateLimitEpoch: submitEpoch,
			MessageHash:    zkcrypto.ShieldMessageHash(tle.BatchSubmissionTypeURL, payload),
			MerkleRoot:     m.path.Root,
			MerkleProof:    m.path,
		})
		require.NoError(t, err)
		require.NotEqual(t, out.Nullifier, sub.Nullifier, "submission and operation nullifiers are unlinkable")
		// A submission whose proof was made for a different payload, or by
		// someone who merely copied the payload, is refused before anything is
		// queued: the submission nullifier belongs to the prover and payload.
		tampered := append([]byte(nil), payload...)
		tampered[len(tampered)-1] ^= 1
		_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
			Submitter:          submitter,
			ExecMode:           types.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH,
			EncryptedPayload:   tampered,
			TargetEpoch:        submitEpoch,
			Proof:              sub.ProofBytes,
			Nullifier:          sub.Nullifier,
			RateLimitNullifier: sub.RateLimitNullifier,
			MerkleRoot:         sub.MerkleRoot,
			ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      1,
		})
		require.ErrorIs(t, err, types.ErrInvalidProof)
		_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
			Submitter:          submitter,
			ExecMode:           types.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH,
			EncryptedPayload:   payload,
			TargetEpoch:        submitEpoch,
			Proof:              sub.ProofBytes,
			Nullifier:          sub.Nullifier,
			RateLimitNullifier: sub.RateLimitNullifier,
			MerkleRoot:         sub.MerkleRoot,
			ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      1,
		})
		require.NoError(t, err)

		return out
	}
	good := submit(ms2[0], submitEpoch)    // bound to the epoch it is submitted in
	wrong := submit(ms2[1], submitEpoch+1) // bound to the epoch it will execute in: must fail

	require.NoError(t, f.keeper.SetShieldEpochDecryptionKey(f.ctx, types.ShieldEpochDecryptionKey{
		Epoch: submitEpoch, DecryptionKey: keyBytes,
	}))

	// Cross the epoch boundary: the EndBlocker advances to epoch 8, then
	// runs epoch 7's batch.
	ctx := f.ctx.WithBlockHeight(startHeight + int64(params.ShieldEpochInterval))
	require.NoError(t, f.keeper.EndBlocker(ctx))
	require.Equal(t, submitEpoch+1, f.keeper.GetCurrentEpoch(ctx))

	var batchEvent *sdk.Event
	for _, ev := range ctx.EventManager().Events() {
		if ev.Type == types.EventTypeShieldBatchExecuted {
			e := ev
			batchEvent = &e
		}
	}
	require.NotNil(t, batchEvent, "batch ran")
	attrs := map[string]string{}
	for _, a := range batchEvent.Attributes {
		attrs[a.Key] = a.Value
	}
	require.Equal(t, "2", attrs[types.AttributeKeyBatchSize])
	require.Equal(t, "1", attrs[types.AttributeKeyProofFailed], "only the wrong-epoch proof fails: %v", attrs)
	require.Equal(t, "0", attrs[types.AttributeKeyDecryptFailed])

	// The correctly bound op got past verification: its nullifier is spent.
	require.True(t, f.keeper.IsNullifierUsed(ctx, domain, 0, fmt.Sprintf("%x", good.Nullifier)))
	require.False(t, f.keeper.IsNullifierUsed(ctx, domain, 0, fmt.Sprintf("%x", wrong.Nullifier)))
}
