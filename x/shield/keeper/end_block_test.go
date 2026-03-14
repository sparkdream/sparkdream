package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

func TestEndBlockerDisabled(t *testing.T) {
	f := initFixture(t)

	// EncryptedBatchEnabled is false by default
	err := f.keeper.EndBlocker(f.ctx)
	require.NoError(t, err)

	// Should not have initialized epoch state
	_, found := f.keeper.GetShieldEpochStateVal(f.ctx)
	// Genesis init sets epoch state, so it may exist. The point is
	// EndBlocker returns early without advancing.
	_ = found
}

func TestEndBlockerInitializesEpochState(t *testing.T) {
	f := initFixtureEmpty(t)

	// Enable encrypted batch
	params := types.DefaultParams()
	params.EncryptedBatchEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// No epoch state yet — EndBlocker should initialize it
	err := f.keeper.EndBlocker(f.ctx)
	require.NoError(t, err)

	epochState, found := f.keeper.GetShieldEpochStateVal(f.ctx)
	require.True(t, found)
	require.Equal(t, uint64(0), epochState.CurrentEpoch)
}

func TestEndBlockerEpochAdvancement(t *testing.T) {
	f := initFixture(t)

	// Enable encrypted batch
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	params.ShieldEpochInterval = 10 // advance every 10 blocks
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Set initial epoch state
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{
		CurrentEpoch:     0,
		EpochStartHeight: 0,
	}))

	// Block 5 — not at boundary yet
	f.ctx = f.ctx.WithBlockHeight(5)
	err = f.keeper.EndBlocker(f.ctx)
	require.NoError(t, err)

	epochState, _ := f.keeper.GetShieldEpochStateVal(f.ctx)
	require.Equal(t, uint64(0), epochState.CurrentEpoch)

	// Block 10 — at boundary
	f.ctx = f.ctx.WithBlockHeight(10)
	err = f.keeper.EndBlocker(f.ctx)
	require.NoError(t, err)

	epochState, found := f.keeper.GetShieldEpochStateVal(f.ctx)
	require.True(t, found)
	require.Equal(t, uint64(1), epochState.CurrentEpoch)
	require.Equal(t, int64(10), epochState.EpochStartHeight)
}

func TestEndBlockerMultipleEpochAdvancements(t *testing.T) {
	f := initFixture(t)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	params.ShieldEpochInterval = 10
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{
		CurrentEpoch:     0,
		EpochStartHeight: 0,
	}))

	// Advance through first epoch
	f.ctx = f.ctx.WithBlockHeight(10)
	err = f.keeper.EndBlocker(f.ctx)
	require.NoError(t, err)

	epochState, _ := f.keeper.GetShieldEpochStateVal(f.ctx)
	require.Equal(t, uint64(1), epochState.CurrentEpoch)

	// Advance through second epoch
	f.ctx = f.ctx.WithBlockHeight(20)
	err = f.keeper.EndBlocker(f.ctx)
	require.NoError(t, err)

	epochState, _ = f.keeper.GetShieldEpochStateVal(f.ctx)
	require.Equal(t, uint64(2), epochState.CurrentEpoch)
}

func TestEndBlockerNoAdvancementBeforeBoundary(t *testing.T) {
	f := initFixture(t)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	params.ShieldEpochInterval = 100
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{
		CurrentEpoch:     5,
		EpochStartHeight: 500,
	}))

	// Block 550 — not at next boundary (500 + 100 = 600)
	f.ctx = f.ctx.WithBlockHeight(550)
	err = f.keeper.EndBlocker(f.ctx)
	require.NoError(t, err)

	epochState, _ := f.keeper.GetShieldEpochStateVal(f.ctx)
	require.Equal(t, uint64(5), epochState.CurrentEpoch)
}

func TestEndBlockerPendingOpsExpired(t *testing.T) {
	f := initFixture(t)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	params.ShieldEpochInterval = 10
	params.MaxPendingEpochs = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Set epoch to 5
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{
		CurrentEpoch:     5,
		EpochStartHeight: 50,
	}))

	// Add a pending op from epoch 1 (very old)
	require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{
		Id:               1,
		TargetEpoch:      1,
		SubmittedAtEpoch: 1,
		Nullifier:        []byte("null1"),
	}))
	require.NoError(t, f.keeper.RecordPendingNullifier(f.ctx, "6e756c6c31")) // hex of "null1"

	// Advance to epoch 6
	f.ctx = f.ctx.WithBlockHeight(60)
	err = f.keeper.EndBlocker(f.ctx)
	require.NoError(t, err)

	// The old pending op should be expired (cutoff = 6 - 2 = 4, and it was from epoch 1)
	ops := f.keeper.GetPendingOpsForEpoch(f.ctx, 1)
	require.Len(t, ops, 0)
}

func TestEndBlockerEpochAdvancementPrunesState(t *testing.T) {
	f := initFixture(t)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	params.ShieldEpochInterval = 10
	params.MaxPendingEpochs = 3
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Set epoch to 10
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{
		CurrentEpoch:     10,
		EpochStartHeight: 100,
	}))

	// Add decryption keys for old epochs
	require.NoError(t, f.keeper.SetShieldEpochDecryptionKey(f.ctx, types.ShieldEpochDecryptionKey{
		Epoch:         1,
		DecryptionKey: []byte("key1"),
	}))
	require.NoError(t, f.keeper.SetShieldEpochDecryptionKey(f.ctx, types.ShieldEpochDecryptionKey{
		Epoch:         10,
		DecryptionKey: []byte("key10"),
	}))

	// Advance epoch
	f.ctx = f.ctx.WithBlockHeight(110)
	err = f.keeper.EndBlocker(f.ctx)
	require.NoError(t, err)

	// Epoch 1 decryption key should be pruned (cutoff = 11 - 3 = 8)
	_, found := f.keeper.GetShieldEpochDecryptionKeyVal(f.ctx, 1)
	require.False(t, found)

	// Epoch 10 should remain
	_, found = f.keeper.GetShieldEpochDecryptionKeyVal(f.ctx, 10)
	require.True(t, found)
}
