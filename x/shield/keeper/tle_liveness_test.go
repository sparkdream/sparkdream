package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

func TestTLELivenessCounterIncrement(t *testing.T) {
	f := initFixture(t)

	// Set up params with liveness checking enabled
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	params.ShieldEpochInterval = 10
	params.TleMissWindow = 10
	params.TleMissTolerance = 5
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Set up TLE key set with validator shares
	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, types.TLEKeySet{
		MasterPublicKey: []byte("master_pk"),
		ValidatorShares: []*types.TLEValidatorPublicShare{
			{ValidatorAddress: "val1", PublicShare: []byte("share1"), ShareIndex: 1},
			{ValidatorAddress: "val2", PublicShare: []byte("share2"), ShareIndex: 2},
		},
	}))

	// Set epoch state
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{
		CurrentEpoch:     1,
		EpochStartHeight: 0,
	}))

	// val1 submits a share, val2 does not
	require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{
		Epoch:     1,
		Validator: "val1",
		Share:     []byte("decryption_share"),
	}))

	// Advance epoch (EndBlocker checks liveness for previous epoch)
	f.ctx = f.ctx.WithBlockHeight(10)
	err = f.keeper.EndBlocker(f.ctx)
	require.NoError(t, err)

	// val1 participated — miss counter should be 0 (reset)
	require.Equal(t, uint64(0), f.keeper.GetTLEMissCount(f.ctx, "val1"))

	// val2 missed — miss counter should be 1
	require.Equal(t, uint64(1), f.keeper.GetTLEMissCount(f.ctx, "val2"))
}

func TestTLELivenessResetOnParticipation(t *testing.T) {
	f := initFixture(t)

	// Pre-set a miss count for val1
	require.NoError(t, f.keeper.SetTLEMissCount(f.ctx, "val1", 3))

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	params.ShieldEpochInterval = 10
	params.TleMissWindow = 10
	params.TleMissTolerance = 5
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, types.TLEKeySet{
		MasterPublicKey: []byte("master_pk"),
		ValidatorShares: []*types.TLEValidatorPublicShare{
			{ValidatorAddress: "val1", PublicShare: []byte("share1"), ShareIndex: 1},
		},
	}))

	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{
		CurrentEpoch:     1,
		EpochStartHeight: 0,
	}))

	// val1 submits a share this time
	require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{
		Epoch:     1,
		Validator: "val1",
		Share:     []byte("share"),
	}))

	f.ctx = f.ctx.WithBlockHeight(10)
	err = f.keeper.EndBlocker(f.ctx)
	require.NoError(t, err)

	// val1 participated — miss counter should be reset to 0
	require.Equal(t, uint64(0), f.keeper.GetTLEMissCount(f.ctx, "val1"))
}

func TestTLELivenessSkippedWhenNoKeySet(t *testing.T) {
	f := initFixture(t)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	params.ShieldEpochInterval = 10
	params.TleMissWindow = 10
	params.TleMissTolerance = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{
		CurrentEpoch:     1,
		EpochStartHeight: 0,
	}))

	// No TLE key set — liveness check should be skipped
	f.ctx = f.ctx.WithBlockHeight(10)
	err = f.keeper.EndBlocker(f.ctx)
	require.NoError(t, err)

	// No miss counters should exist
	require.Equal(t, uint64(0), f.keeper.GetTLEMissCount(f.ctx, "val1"))
}

func TestTLELivenessSkippedWhenToleranceZero(t *testing.T) {
	f := initFixture(t)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	params.ShieldEpochInterval = 10
	params.TleMissWindow = 0    // Disabled
	params.TleMissTolerance = 0 // Disabled
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, types.TLEKeySet{
		MasterPublicKey: []byte("master_pk"),
		ValidatorShares: []*types.TLEValidatorPublicShare{
			{ValidatorAddress: "val1", PublicShare: []byte("share1"), ShareIndex: 1},
		},
	}))

	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{
		CurrentEpoch:     1,
		EpochStartHeight: 0,
	}))

	// No shares submitted
	f.ctx = f.ctx.WithBlockHeight(10)
	err = f.keeper.EndBlocker(f.ctx)
	require.NoError(t, err)

	// With tolerance=0, liveness checks are skipped entirely
	require.Equal(t, uint64(0), f.keeper.GetTLEMissCount(f.ctx, "val1"))
}

func TestTLELivenessMultipleMisses(t *testing.T) {
	f := initFixture(t)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	params.ShieldEpochInterval = 10
	params.TleMissWindow = 100
	params.TleMissTolerance = 10 // High tolerance — no jailing
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, types.TLEKeySet{
		MasterPublicKey: []byte("master_pk"),
		ValidatorShares: []*types.TLEValidatorPublicShare{
			{ValidatorAddress: "val1", PublicShare: []byte("share1"), ShareIndex: 1},
		},
	}))

	// Simulate multiple epochs where val1 misses
	for epoch := uint64(0); epoch < 3; epoch++ {
		require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{
			CurrentEpoch:     epoch,
			EpochStartHeight: int64(epoch * 10),
		}))

		f.ctx = f.ctx.WithBlockHeight(int64((epoch + 1) * 10))
		err = f.keeper.EndBlocker(f.ctx)
		require.NoError(t, err)
	}

	// val1 should have accumulated 3 misses
	require.Equal(t, uint64(3), f.keeper.GetTLEMissCount(f.ctx, "val1"))
}

// TestTLELivenessSlidingWindowWraparound verifies that the ring buffer correctly
// overwrites old slots once completedEpoch >= window. This is the behavior
// replacing the old simple miss accumulator — a miss that aged past the window
// must stop counting toward jailing.
func TestTLELivenessSlidingWindowWraparound(t *testing.T) {
	f := initFixture(t)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	params.ShieldEpochInterval = 10
	params.TleMissWindow = 3
	params.TleMissTolerance = 10 // high — we're measuring counts, not jailing
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, types.TLEKeySet{
		MasterPublicKey: []byte("master_pk"),
		ValidatorShares: []*types.TLEValidatorPublicShare{
			{ValidatorAddress: "val1", PublicShare: []byte("share1"), ShareIndex: 1},
		},
	}))

	// Miss epochs 0, 1, 2 — ring buffer fully filled with misses.
	for epoch := uint64(0); epoch < 3; epoch++ {
		require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{
			CurrentEpoch:     epoch,
			EpochStartHeight: int64(epoch * 10),
		}))
		f.ctx = f.ctx.WithBlockHeight(int64((epoch + 1) * 10))
		require.NoError(t, f.keeper.EndBlocker(f.ctx))
	}
	require.Equal(t, uint64(3), f.keeper.GetTLEMissCount(f.ctx, "val1"))

	// Epoch 3 participates — slot 3%3 = 0 is overwritten with participated=true.
	// Expected misses: slots 1 and 2 still missed → 2.
	require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{
		Epoch:     3,
		Validator: "val1",
		Share:     []byte("decryption_share"),
	}))
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{
		CurrentEpoch:     3,
		EpochStartHeight: 30,
	}))
	f.ctx = f.ctx.WithBlockHeight(40)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))

	require.Equal(t, uint64(2), f.keeper.GetTLEMissCount(f.ctx, "val1"),
		"slot 0 should have been overwritten with a participation, leaving only slots 1 and 2 as misses")
}

// TestTLELivenessToleranceExceededClearsWindow verifies that when a validator
// exceeds TleMissTolerance the ring buffer is cleared (giving a fresh start)
// even if the staking/slashing keepers are not wired — clearTLEParticipation
// runs unconditionally after the jail call.
func TestTLELivenessToleranceExceededClearsWindow(t *testing.T) {
	f := initFixture(t)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	params.ShieldEpochInterval = 10
	params.TleMissWindow = 5
	params.TleMissTolerance = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, types.TLEKeySet{
		MasterPublicKey: []byte("master_pk"),
		ValidatorShares: []*types.TLEValidatorPublicShare{
			{ValidatorAddress: "val1", PublicShare: []byte("share1"), ShareIndex: 1},
		},
	}))

	// Miss 3 epochs — exceeds tolerance of 2, which triggers jail + buffer clear.
	for epoch := uint64(0); epoch < 3; epoch++ {
		require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{
			CurrentEpoch:     epoch,
			EpochStartHeight: int64(epoch * 10),
		}))
		f.ctx = f.ctx.WithBlockHeight(int64((epoch + 1) * 10))
		require.NoError(t, f.keeper.EndBlocker(f.ctx))
	}

	// After the 3rd miss triggered the clear path, the next missed epoch should
	// start counting from a fresh buffer: exactly 1 miss in the window.
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{
		CurrentEpoch:     3,
		EpochStartHeight: 30,
	}))
	f.ctx = f.ctx.WithBlockHeight(40)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))

	require.Equal(t, uint64(1), f.keeper.GetTLEMissCount(f.ctx, "val1"),
		"ring buffer should have been cleared after tolerance was exceeded")
}
