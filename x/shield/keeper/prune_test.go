package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

// TestPruneIntegrated tests the integrated pruning behavior that pruneStaleState
// orchestrates. Since pruneStaleState is unexported, we test the same scenario
// via the individual public prune methods with coordinated parameters.
func TestPruneIntegrated(t *testing.T) {
	f := initFixture(t)

	// Set up state across multiple collections at "old" epochs/days.
	// Epoch-scoped nullifiers (domain 1 = blog posts, EPOCH scoped)
	require.NoError(t, f.keeper.RecordNullifier(f.ctx, 1, 2, "old_null", 10))
	require.NoError(t, f.keeper.RecordNullifier(f.ctx, 1, 10, "current_null", 50))

	// Identity rate limits
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 2}))
	f.keeper.CheckAndIncrementRateLimit(f.ctx, "old_id", 100)

	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 10}))
	f.keeper.CheckAndIncrementRateLimit(f.ctx, "current_id", 100)

	// Day fundings
	require.NoError(t, f.keeper.SetDayFunding(f.ctx, 1, math.NewInt(100)))
	require.NoError(t, f.keeper.SetDayFunding(f.ctx, 5, math.NewInt(500)))

	// Decryption state
	require.NoError(t, f.keeper.SetShieldEpochDecryptionKey(f.ctx, types.ShieldEpochDecryptionKey{
		Epoch: 2, DecryptionKey: []byte("old_key"),
	}))
	require.NoError(t, f.keeper.SetShieldEpochDecryptionKey(f.ctx, types.ShieldEpochDecryptionKey{
		Epoch: 10, DecryptionKey: []byte("current_key"),
	}))

	// Simulate pruning at epoch 10 with cutoff=5
	cutoffEpoch := uint64(5)

	require.NoError(t, f.keeper.PruneIdentityRateLimits(f.ctx, cutoffEpoch))
	require.NoError(t, f.keeper.PruneEpochScopedNullifiers(f.ctx, cutoffEpoch))
	require.NoError(t, f.keeper.PruneDayFundings(f.ctx, 4)) // Keep day 5+
	require.NoError(t, f.keeper.PruneDecryptionState(f.ctx, cutoffEpoch))

	// Verify old state pruned
	require.False(t, f.keeper.IsNullifierUsed(f.ctx, 1, 2, "old_null"))
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 2}))
	require.Equal(t, uint64(0), f.keeper.GetIdentityRateLimitCount(f.ctx, "old_id"))
	require.True(t, f.keeper.GetDayFunding(f.ctx, 1).IsZero())
	_, found := f.keeper.GetShieldEpochDecryptionKeyVal(f.ctx, 2)
	require.False(t, found)

	// Verify current state preserved
	require.True(t, f.keeper.IsNullifierUsed(f.ctx, 1, 10, "current_null"))
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 10}))
	require.Equal(t, uint64(1), f.keeper.GetIdentityRateLimitCount(f.ctx, "current_id"))
	require.Equal(t, math.NewInt(500), f.keeper.GetDayFunding(f.ctx, 5))
	_, found = f.keeper.GetShieldEpochDecryptionKeyVal(f.ctx, 10)
	require.True(t, found)
}

func TestPruneEarlyEpochs(t *testing.T) {
	f := initFixture(t)

	// Test pruning behavior at epoch 0 and 1 (guard against underflow)
	// All prune methods should handle cutoff=0 gracefully
	require.NoError(t, f.keeper.PruneIdentityRateLimits(f.ctx, 0))
	require.NoError(t, f.keeper.PruneEpochScopedNullifiers(f.ctx, 0))
	require.NoError(t, f.keeper.PruneDayFundings(f.ctx, 0))
	require.NoError(t, f.keeper.PruneDecryptionState(f.ctx, 0))
}
