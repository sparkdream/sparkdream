package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

func TestRateLimitBoundary(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 1}))

	// Max of 1 — first call succeeds, second fails
	require.True(t, f.keeper.CheckAndIncrementRateLimit(f.ctx, "single_use", 1))
	require.False(t, f.keeper.CheckAndIncrementRateLimit(f.ctx, "single_use", 1))
	require.Equal(t, uint64(1), f.keeper.GetIdentityRateLimitCount(f.ctx, "single_use"))
}

func TestRateLimitEpochIsolation(t *testing.T) {
	f := initFixture(t)

	// Use up limit in epoch 1
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 1}))
	require.True(t, f.keeper.CheckAndIncrementRateLimit(f.ctx, "id_epoch", 1))
	require.False(t, f.keeper.CheckAndIncrementRateLimit(f.ctx, "id_epoch", 1))

	// Move to epoch 2 — limit should reset
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 2}))
	require.True(t, f.keeper.CheckAndIncrementRateLimit(f.ctx, "id_epoch", 1))
	require.Equal(t, uint64(1), f.keeper.GetIdentityRateLimitCount(f.ctx, "id_epoch"))
}

func TestRateLimitZeroMaxAlwaysFails(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 1}))

	// maxPerEpoch=0 means no operations allowed
	require.False(t, f.keeper.CheckAndIncrementRateLimit(f.ctx, "zero_max", 0))
}

func TestPruneIdentityRateLimitsEmpty(t *testing.T) {
	f := initFixture(t)

	// Prune on empty state should not error
	err := f.keeper.PruneIdentityRateLimits(f.ctx, 100)
	require.NoError(t, err)
}

func TestPruneIdentityRateLimitsAll(t *testing.T) {
	f := initFixture(t)

	// Create entries at epochs 1 and 2
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 1}))
	f.keeper.CheckAndIncrementRateLimit(f.ctx, "id1", 100)

	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 2}))
	f.keeper.CheckAndIncrementRateLimit(f.ctx, "id2", 100)

	// Prune everything (cutoff > all epochs)
	err := f.keeper.PruneIdentityRateLimits(f.ctx, 100)
	require.NoError(t, err)

	// All should be gone
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 1}))
	require.Equal(t, uint64(0), f.keeper.GetIdentityRateLimitCount(f.ctx, "id1"))
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 2}))
	require.Equal(t, uint64(0), f.keeper.GetIdentityRateLimitCount(f.ctx, "id2"))
}

func TestGetIdentityRateLimitCountDefaultEpoch(t *testing.T) {
	f := initFixture(t)

	// Without setting epoch state, epoch defaults to 0
	count := f.keeper.GetIdentityRateLimitCount(f.ctx, "any_id")
	require.Equal(t, uint64(0), count)
}
