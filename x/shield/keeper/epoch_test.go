package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

func TestEpochStateNotFoundDefault(t *testing.T) {
	f := initFixtureEmpty(t)

	// Without genesis, epoch state should return not found
	_, found := f.keeper.GetShieldEpochStateVal(f.ctx)
	require.False(t, found)

	// GetCurrentEpoch should return 0 when not found
	require.Equal(t, uint64(0), f.keeper.GetCurrentEpoch(f.ctx))
}

func TestEpochStateZeroValues(t *testing.T) {
	f := initFixture(t)

	// Setting all zero values should still be retrievable
	state := types.ShieldEpochState{
		CurrentEpoch:     0,
		EpochStartHeight: 0,
	}
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, state))

	got, found := f.keeper.GetShieldEpochStateVal(f.ctx)
	require.True(t, found)
	require.Equal(t, uint64(0), got.CurrentEpoch)
}

func TestDecryptionKeySetAndGet(t *testing.T) {
	f := initFixture(t)

	dk := types.ShieldEpochDecryptionKey{
		Epoch:                 5,
		DecryptionKey:         []byte("epoch5_key"),
		ReconstructedAtHeight: 250,
	}
	require.NoError(t, f.keeper.SetShieldEpochDecryptionKey(f.ctx, dk))

	got, found := f.keeper.GetShieldEpochDecryptionKeyVal(f.ctx, 5)
	require.True(t, found)
	require.Equal(t, []byte("epoch5_key"), got.DecryptionKey)
	require.Equal(t, int64(250), got.ReconstructedAtHeight)
}

func TestDecryptionKeyNotFound(t *testing.T) {
	f := initFixture(t)

	_, found := f.keeper.GetShieldEpochDecryptionKeyVal(f.ctx, 999)
	require.False(t, found)
}

func TestPruneDecryptionStateWithShares(t *testing.T) {
	f := initFixture(t)

	// Add decryption keys AND shares for epochs 1 and 5
	require.NoError(t, f.keeper.SetShieldEpochDecryptionKey(f.ctx, types.ShieldEpochDecryptionKey{
		Epoch: 1, DecryptionKey: []byte("key1"),
	}))
	require.NoError(t, f.keeper.SetShieldEpochDecryptionKey(f.ctx, types.ShieldEpochDecryptionKey{
		Epoch: 5, DecryptionKey: []byte("key5"),
	}))
	require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{
		Epoch: 1, Validator: "val1", Share: []byte("share1_1"),
	}))
	require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{
		Epoch: 1, Validator: "val2", Share: []byte("share1_2"),
	}))
	require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{
		Epoch: 5, Validator: "val1", Share: []byte("share5_1"),
	}))

	// Prune epochs < 5
	err := f.keeper.PruneDecryptionState(f.ctx, 5)
	require.NoError(t, err)

	// Epoch 1 key and shares should be pruned
	_, found := f.keeper.GetShieldEpochDecryptionKeyVal(f.ctx, 1)
	require.False(t, found)
	_, found = f.keeper.GetDecryptionShare(f.ctx, 1, "val1")
	require.False(t, found)
	_, found = f.keeper.GetDecryptionShare(f.ctx, 1, "val2")
	require.False(t, found)

	// Epoch 5 should remain
	_, found = f.keeper.GetShieldEpochDecryptionKeyVal(f.ctx, 5)
	require.True(t, found)
	_, found = f.keeper.GetDecryptionShare(f.ctx, 5, "val1")
	require.True(t, found)
}

func TestPruneDecryptionStateEmpty(t *testing.T) {
	f := initFixture(t)

	err := f.keeper.PruneDecryptionState(f.ctx, 100)
	require.NoError(t, err)
}

func TestDecryptionKeyOverwrite(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.SetShieldEpochDecryptionKey(f.ctx, types.ShieldEpochDecryptionKey{
		Epoch: 3, DecryptionKey: []byte("old_key"),
	}))
	require.NoError(t, f.keeper.SetShieldEpochDecryptionKey(f.ctx, types.ShieldEpochDecryptionKey{
		Epoch: 3, DecryptionKey: []byte("new_key"),
	}))

	got, found := f.keeper.GetShieldEpochDecryptionKeyVal(f.ctx, 3)
	require.True(t, found)
	require.Equal(t, []byte("new_key"), got.DecryptionKey)
}
