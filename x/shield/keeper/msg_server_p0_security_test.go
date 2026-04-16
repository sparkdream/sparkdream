package keeper_test

import (
	"encoding/hex"
	"fmt"
	"testing"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	any "github.com/cosmos/gogoproto/types/any"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

// TestShieldedExecRateLimitExhaustion verifies that the rate limit is enforced
// end-to-end through the ShieldedExec handler (not just via direct method call).
// This is P0 security: prevents DoS via free shielded execution.
// Uses encrypted batch mode since immediate mode requires a VK to be stored.
func TestShieldedExecRateLimitExhaustion(t *testing.T) {
	f, ms := initMsgServer(t)

	submitter, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("test"))
	require.NoError(t, err)

	// Set epoch
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 1}))

	// Enable encrypted batch with rate limit of 3
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxExecsPerIdentityPerEpoch = 3
	params.EncryptedBatchEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Use the same rate limit nullifier for all calls
	sameRateLimit := make([]byte, 32)
	copy(sameRateLimit, "same_rate_limit_identity_key")

	// Submit 3 successful shielded execs (each with unique nullifier but same rate limit identity)
	for i := 0; i < 3; i++ {
		uniqueNullifier := make([]byte, 32)
		copy(uniqueNullifier, fmt.Sprintf("unique_nullifier_%d_padding", i))

		_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
			Submitter:          submitter,
			ExecMode:           types.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH,
			EncryptedPayload:   []byte("encrypted_data"),
			TargetEpoch:        1,
			Nullifier:          uniqueNullifier,
			RateLimitNullifier: sameRateLimit,
			MerkleRoot:         make([]byte, 32),
			ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      1,
		})
		require.NoError(t, err, "submission %d should succeed", i)
	}

	// 4th call should hit rate limit
	uniqueNullifier4 := make([]byte, 32)
	copy(uniqueNullifier4, "unique_nullifier_3_padding")

	_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
		Submitter:          submitter,
		ExecMode:           types.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH,
		EncryptedPayload:   []byte("encrypted_data"),
		TargetEpoch:        1,
		Nullifier:          uniqueNullifier4,
		RateLimitNullifier: sameRateLimit,
		MerkleRoot:         make([]byte, 32),
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		MinTrustLevel:      1,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrRateLimitExceeded)

	// Verify rate limit count via query
	rateLimitHex := hex.EncodeToString(sameRateLimit)
	count := f.keeper.GetIdentityRateLimitCount(f.ctx, rateLimitHex)
	require.Equal(t, uint64(3), count)
}

// TestShieldedExecRateLimitEpochReset verifies that rate limits reset when
// the epoch advances. This is essential for the rate limit system to not
// permanently block identities.
func TestShieldedExecRateLimitEpochReset(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 1}))

	rateLimitHex := "rate_limit_identity_for_epoch_reset_test"

	// Exhaust rate limit in epoch 1
	require.True(t, f.keeper.CheckAndIncrementRateLimit(f.ctx, rateLimitHex, 2))
	require.True(t, f.keeper.CheckAndIncrementRateLimit(f.ctx, rateLimitHex, 2))
	require.False(t, f.keeper.CheckAndIncrementRateLimit(f.ctx, rateLimitHex, 2))

	// Advance epoch
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 2}))

	// Rate limit should be reset — can submit again
	require.True(t, f.keeper.CheckAndIncrementRateLimit(f.ctx, rateLimitHex, 2))
	require.Equal(t, uint64(1), f.keeper.GetIdentityRateLimitCount(f.ctx, rateLimitHex))
}

// TestEncryptedBatchSubmissionHappyPath verifies that a valid encrypted batch
// payload is successfully queued. This is the ONLY happy-path test for the
// encrypted batch code path (all other tests are error cases).
func TestEncryptedBatchSubmissionHappyPath(t *testing.T) {
	f, ms := initMsgServer(t)

	submitter, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("test"))
	require.NoError(t, err)

	// Enable encrypted batch
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Set epoch
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 5}))

	nullifier := make([]byte, 32)
	copy(nullifier, "nullifier_for_batch_happy_path")
	rateLimitNull := make([]byte, 32)
	copy(rateLimitNull, "rate_limit_batch_happy_path")

	resp, err := ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
		Submitter:          submitter,
		ExecMode:           types.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH,
		EncryptedPayload:   []byte("encrypted_vote_payload_data"),
		TargetEpoch:        5,
		Nullifier:          nullifier,
		RateLimitNullifier: rateLimitNull,
		MerkleRoot:         make([]byte, 32),
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		MinTrustLevel:      1,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify the op was queued
	pendingCount := f.keeper.GetPendingOpCountVal(f.ctx)
	require.Equal(t, uint64(1), pendingCount)

	// Verify the pending nullifier is recorded
	require.True(t, f.keeper.IsPendingNullifier(f.ctx, hex.EncodeToString(nullifier)))

	// Verify rate limit was incremented
	rateLimitHex := hex.EncodeToString(rateLimitNull)
	count := f.keeper.GetIdentityRateLimitCount(f.ctx, rateLimitHex)
	require.Equal(t, uint64(1), count)
}

// TestEncryptedBatchNullifierReplay verifies that submitting the same nullifier
// twice in encrypted batch mode is rejected (ErrNullifierUsed).
func TestEncryptedBatchNullifierReplay(t *testing.T) {
	f, ms := initMsgServer(t)

	submitter, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("test"))
	require.NoError(t, err)

	// Enable encrypted batch
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Set epoch
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 1}))

	sameNullifier := make([]byte, 32)
	copy(sameNullifier, "same_nullifier_for_replay_test")

	// First submission succeeds
	_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
		Submitter:          submitter,
		ExecMode:           types.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH,
		EncryptedPayload:   []byte("first_encrypted_vote"),
		TargetEpoch:        1,
		Nullifier:          sameNullifier,
		RateLimitNullifier: func() []byte { b := make([]byte, 32); copy(b, "rate_1_replay"); return b }(),
		MerkleRoot:         make([]byte, 32),
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
	})
	require.NoError(t, err)

	// Second submission with same nullifier should fail
	_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
		Submitter:          submitter,
		ExecMode:           types.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH,
		EncryptedPayload:   []byte("second_encrypted_vote"),
		TargetEpoch:        1,
		Nullifier:          sameNullifier,
		RateLimitNullifier: func() []byte { b := make([]byte, 32); copy(b, "rate_2_replay"); return b }(),
		MerkleRoot:         make([]byte, 32),
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrNullifierUsed)
}

// TestEncryptedBatchRateLimitExhaustion verifies rate limits work in
// encrypted batch mode too, not just immediate mode.
func TestEncryptedBatchRateLimitExhaustion(t *testing.T) {
	f, ms := initMsgServer(t)

	submitter, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("test"))
	require.NoError(t, err)

	// Enable encrypted batch with rate limit of 2
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	params.MaxExecsPerIdentityPerEpoch = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 1}))

	sameRateLimit := make([]byte, 32)
	copy(sameRateLimit, "same_rate_limit_batch_exhaust")

	// Two successful submissions
	for i := 0; i < 2; i++ {
		uniqueNull := make([]byte, 32)
		copy(uniqueNull, fmt.Sprintf("batch_null_%d_padding__________", i))

		_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
			Submitter:          submitter,
			ExecMode:           types.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH,
			EncryptedPayload:   []byte("encrypted_data"),
			TargetEpoch:        1,
			Nullifier:          uniqueNull,
			RateLimitNullifier: sameRateLimit,
			MerkleRoot:         make([]byte, 32),
			ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		})
		require.NoError(t, err, "submission %d should succeed", i)
	}

	// Third should be rate-limited
	thirdNull := make([]byte, 32)
	copy(thirdNull, "batch_null_2_padding__________")

	_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
		Submitter:          submitter,
		ExecMode:           types.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH,
		EncryptedPayload:   []byte("encrypted_data"),
		TargetEpoch:        1,
		Nullifier:          thirdNull,
		RateLimitNullifier: sameRateLimit,
		MerkleRoot:         make([]byte, 32),
		ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrRateLimitExceeded)
}

// TestImmediateNotAllowedForEncryptedOnlyOp verifies that operations registered
// with SHIELD_BATCH_MODE_ENCRYPTED_ONLY cannot be executed in immediate mode.
// (This is already tested in msg_server_shielded_exec_test.go but included here
// for completeness as a P0 security item.)
func TestImmediateNotAllowedForEncryptedOnlyOp(t *testing.T) {
	f, ms := initMsgServer(t)

	submitter, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("test"))
	require.NoError(t, err)

	// Register an ENCRYPTED_ONLY operation
	require.NoError(t, f.keeper.SetShieldedOp(f.ctx, types.ShieldedOpRegistration{
		MessageTypeUrl: "/sparkdream.commons.v1.MsgAnonymousVoteProposal",
		ProofDomain:    types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		MinTrustLevel:  1,
		Active:         true,
		BatchMode:      types.ShieldBatchMode_SHIELD_BATCH_MODE_ENCRYPTED_ONLY,
	}))

	// Try immediate execution — should be rejected
	_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
		Submitter: submitter,
		ExecMode:  types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE,
		InnerMessage: &any.Any{
			TypeUrl: "/sparkdream.commons.v1.MsgAnonymousVoteProposal",
			Value:   []byte("vote_data"),
		},
		ProofDomain:   types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
		MinTrustLevel: 1,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrImmediateNotAllowed)
}
