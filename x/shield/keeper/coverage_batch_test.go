package keeper_test

import (
	"encoding/hex"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4/util/random"

	"sparkdream/tools/tle"
	"sparkdream/x/shield/keeper"
	"sparkdream/x/shield/types"
)

// batchPrecheckFixture enables batch mode at epoch 4 and returns a valid
// encrypted-batch submission (no VK stored, so the submission proof is
// skipped under testparams).
func batchPrecheckFixture(t *testing.T) (*fixture, *types.MsgShieldedExec) {
	t.Helper()
	f := initFixture(t)
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 4, EpochStartHeight: 40}))
	nullifier := make([]byte, 32)
	nullifier[0] = 0xab
	return f, &types.MsgShieldedExec{
		ExecMode:           types.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH,
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		EncryptedPayload:   []byte("ciphertext"),
		Nullifier:          nullifier,
		RateLimitNullifier: make([]byte, 32),
		MerkleRoot:         make([]byte, 32),
		TargetEpoch:        4,
	}
}

// TestPrecheckBatchReservesSubmissionNullifierInCheckTx mirrors
// TestPrecheckImmediateReservesNullifierInCheckTx for batch submissions: in
// CheckTx the submission nullifier is reserved as pending so a rewrapped copy
// is refused; simulate and DeliverTx do not reserve.
func TestPrecheckBatchReservesSubmissionNullifierInCheckTx(t *testing.T) {
	t.Run("check tx reserves", func(t *testing.T) {
		f, msg := batchPrecheckFixture(t)
		checkCtx := f.ctx.WithIsCheckTx(true)
		require.NoError(t, f.keeper.PrecheckExec(checkCtx, msg))
		require.True(t, f.keeper.IsPendingNullifier(checkCtx, hex.EncodeToString(msg.Nullifier)))
		require.ErrorIs(t, f.keeper.PrecheckExec(checkCtx, msg), types.ErrInvalidSubmissionNullifier)
	})
	t.Run("simulate does not reserve", func(t *testing.T) {
		f, msg := batchPrecheckFixture(t)
		simCtx := f.ctx.WithIsCheckTx(true).WithExecMode(sdk.ExecModeSimulate)
		require.NoError(t, f.keeper.PrecheckExec(simCtx, msg))
		require.False(t, f.keeper.IsPendingNullifier(simCtx, hex.EncodeToString(msg.Nullifier)))
		require.NoError(t, f.keeper.PrecheckExec(simCtx, msg))
	})
	t.Run("deliver tx does not reserve", func(t *testing.T) {
		f, msg := batchPrecheckFixture(t)
		require.NoError(t, f.keeper.PrecheckExec(f.ctx, msg))
		require.False(t, f.keeper.IsPendingNullifier(f.ctx, hex.EncodeToString(msg.Nullifier)))
	})
}

// TestPrecheckBatchNullifierLengths: both batch nullifiers must be 32 bytes.
func TestPrecheckBatchNullifierLengths(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*types.MsgShieldedExec)
	}{
		{"short nullifier", func(m *types.MsgShieldedExec) { m.Nullifier = make([]byte, 31) }},
		{"long nullifier", func(m *types.MsgShieldedExec) { m.Nullifier = make([]byte, 33) }},
		{"missing nullifier", func(m *types.MsgShieldedExec) { m.Nullifier = nil }},
		{"short rate-limit nullifier", func(m *types.MsgShieldedExec) { m.RateLimitNullifier = make([]byte, 16) }},
		{"missing rate-limit nullifier", func(m *types.MsgShieldedExec) { m.RateLimitNullifier = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, msg := batchPrecheckFixture(t)
			tc.mutate(msg)
			checkCtx := f.ctx.WithIsCheckTx(true)
			require.ErrorIs(t, f.keeper.PrecheckExec(checkCtx, msg), types.ErrInvalidNullifierLength)
			require.Zero(t, f.keeper.GetPendingOpCountVal(checkCtx))
		})
	}
}

// TestExecuteBatchHonorsMaxOpsPerBatch: with max_ops_per_batch = 2, a
// demanded epoch of three ops runs two in the first block and leaves one.
func TestExecuteBatchHonorsMaxOpsPerBatch(t *testing.T) {
	f := initFixture(t)
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	params.ShieldEpochInterval = 10
	params.MinBatchSize = 3
	params.MaxPendingEpochs = 4
	params.MaxOpsPerBatch = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 5, EpochStartHeight: 50}))
	for id := uint64(1); id <= 3; id++ {
		require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{
			Id: id, TargetEpoch: 4, SubmittedAtEpoch: 4,
			SubmissionNullifier: []byte{byte(id)}, EncryptedPayload: []byte("x"),
		}))
	}
	require.NoError(t, f.keeper.SetShieldEpochDecryptionKey(f.ctx, types.ShieldEpochDecryptionKey{Epoch: 4, DecryptionKey: []byte("seeded")}))

	ctx := f.ctx.WithBlockHeight(55).WithEventManager(sdk.NewEventManager())
	require.NoError(t, f.keeper.EndBlocker(ctx))
	require.Equal(t, uint64(1), f.keeper.GetPendingOpCountVal(ctx), "one op left for the next block")
	require.Len(t, f.keeper.GetPendingOpsForEpoch(ctx, 4), 1)

	var batchSize string
	for _, ev := range ctx.EventManager().Events() {
		if ev.Type == types.EventTypeShieldBatchExecuted {
			for _, a := range ev.Attributes {
				if a.Key == types.AttributeKeyBatchSize {
					batchSize = a.Value
				}
			}
		}
	}
	require.Equal(t, "2", batchSize)

	// The next block runs the remainder.
	ctx = ctx.WithBlockHeight(56)
	require.NoError(t, f.keeper.EndBlocker(ctx))
	require.Zero(t, f.keeper.GetPendingOpCountVal(ctx))
}

// TestProcessEncryptedOpRejections: each rejection branch of a decrypted
// batch op drops it as "other" failure and runs nothing; the baseline op
// executes.
func TestProcessEncryptedOpRejections(t *testing.T) {
	const epoch = uint64(3)
	const domain = uint32(11)
	opNullifier := make([]byte, 32)
	opNullifier[0] = 0x42

	cases := []struct {
		name          string
		fragNullifier []byte
		batchMode     types.ShieldBatchMode
		regTrust      uint32
		opTrust       uint32
		preUsed       bool
		want          int
	}{
		{"baseline executes", opNullifier, types.ShieldBatchMode_SHIELD_BATCH_MODE_EITHER, 1, 1, false, keeper.BatchResultOK},
		{"fragment nullifier not 32 bytes", opNullifier[:31], types.ShieldBatchMode_SHIELD_BATCH_MODE_EITHER, 1, 1, false, keeper.BatchResultOtherFail},
		{"trust level below registration", opNullifier, types.ShieldBatchMode_SHIELD_BATCH_MODE_EITHER, 2, 1, false, keeper.BatchResultOtherFail},
		{"immediate-only registration", opNullifier, types.ShieldBatchMode_SHIELD_BATCH_MODE_IMMEDIATE_ONLY, 1, 1, false, keeper.BatchResultOtherFail},
		{"operation nullifier already used", opNullifier, types.ShieldBatchMode_SHIELD_BATCH_MODE_EITHER, 1, 1, true, keeper.BatchResultOtherFail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			f.keeper.SetRouter(writeThenRouter{k: f.keeper, burn: 1_000})
			f.keeper.RegisterShieldAwareModule("/sparkdream.shield.v1.", &ownedModule{})

			inner := innerAny(t, &types.MsgTriggerDkg{Authority: authtypes.NewModuleAddress(types.ModuleName).String()})
			require.NoError(t, f.keeper.SetShieldedOp(f.ctx, types.ShieldedOpRegistration{
				MessageTypeUrl:     inner.TypeUrl,
				ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
				MinTrustLevel:      tc.regTrust,
				NullifierDomain:    domain,
				NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_GLOBAL,
				Active:             true,
				BatchMode:          tc.batchMode,
			}))
			if tc.preUsed {
				require.NoError(t, f.keeper.RecordNullifier(f.ctx, domain, 0, hex.EncodeToString(opNullifier), 1))
			}

			plaintext, err := (&types.MsgShieldedExec{InnerMessage: inner, Proof: []byte("proof"), Nullifier: tc.fragNullifier}).Marshal()
			require.NoError(t, err)
			master := tle.Suite.G1().Scalar().Pick(random.New())
			payload, err := tle.Encrypt(tle.MasterPublicKeyG2(master), epoch, plaintext)
			require.NoError(t, err)
			op := &types.PendingShieldedOp{
				Id:                 1,
				TargetEpoch:        epoch,
				EncryptedPayload:   payload,
				MerkleRoot:         make([]byte, 32),
				ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
				MinTrustLevel:      tc.opTrust,
				RateLimitNullifier: make([]byte, 32),
				SubmittedAtEpoch:   epoch,
			}

			result := f.keeper.ProcessEncryptedOp(f.ctx, types.DefaultParams(), op, tle.EpochDecryptionKey(master, epoch))
			require.Equal(t, tc.want, result)

			_, ran := f.keeper.GetShieldEpochDecryptionKeyVal(f.ctx, markerEpoch)
			require.Equal(t, tc.want == keeper.BatchResultOK, ran, "inner message runs only for an accepted op")
			if tc.want == keeper.BatchResultOK {
				require.True(t, f.keeper.IsNullifierUsed(f.ctx, domain, 0, hex.EncodeToString(opNullifier)))
			}
		})
	}
}
