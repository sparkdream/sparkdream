package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
	reptypes "sparkdream/x/rep/types"
)

func TestSyncCuratorBondedRoleConfig_WritesThroughToRep(t *testing.T) {
	f := initTestFixture(t)

	// Default params → expect a config with the collect defaults mapped onto
	// the rep BondedRoleConfig shape.
	params := types.DefaultParams()
	require.NoError(t, f.keeper.SyncCuratorBondedRoleConfig(f.ctx, params))

	cfg, ok := f.repKeeper.bondedRoleConfigs[reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR]
	require.True(t, ok, "rep mock must have received the curator config")
	require.Equal(t, reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR, cfg.RoleType)
	require.Equal(t, params.MinCuratorBond.String(), cfg.MinBond)
	require.Equal(t, uint64(0), cfg.MinRepTier, "curator is trust-level-gated, not rep-tier-gated")
	require.Equal(t, params.MinCuratorTrustLevel, cfg.MinTrustLevel)
	require.Equal(t, params.MinCuratorAgeBlocks, cfg.MinAgeBlocks)
	require.Equal(t, params.CuratorDemotionCooldown, cfg.DemotionCooldown)
	require.Equal(t, params.CuratorDemotionThreshold.String(), cfg.DemotionThreshold)
}

func TestSyncCuratorBondedRoleConfig_NilAmountsNormalizeToZero(t *testing.T) {
	f := initTestFixture(t)

	// Construct params with nil math.Int fields to exercise the defensive
	// nil-check path in the sync helper.
	params := types.Params{
		MinCuratorTrustLevel:     "TRUST_LEVEL_ESTABLISHED",
		MinCuratorAgeBlocks:      100,
		CuratorDemotionCooldown:  3600,
		// MinCuratorBond and CuratorDemotionThreshold are intentionally left
		// as the proto zero value (nil math.Int) to hit the branch.
	}
	require.NoError(t, f.keeper.SyncCuratorBondedRoleConfig(f.ctx, params))

	cfg := f.repKeeper.bondedRoleConfigs[reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR]
	require.Equal(t, "0", cfg.MinBond)
	require.Equal(t, "0", cfg.DemotionThreshold)
	require.Equal(t, "TRUST_LEVEL_ESTABLISHED", cfg.MinTrustLevel)
}

func TestSyncCuratorBondedRoleConfig_NoRepKeeperIsNoop(t *testing.T) {
	f := initTestFixture(t)

	// Clear the rep keeper wiring (simulates standalone test construction
	// where SetRepKeeper hasn't been called).
	f.keeper.SetRepKeeper(nil)

	// Should return nil without panicking and without writing.
	require.NoError(t, f.keeper.SyncCuratorBondedRoleConfig(f.ctx, types.DefaultParams()))
	require.Empty(t, f.repKeeper.bondedRoleConfigs,
		"no write-through expected when rep keeper is unwired")
}

func TestSyncCuratorBondedRoleConfig_OverwritesExisting(t *testing.T) {
	f := initTestFixture(t)

	// Seed a stale config on the rep side; the sync should replace it.
	f.repKeeper.bondedRoleConfigs = map[reptypes.RoleType]reptypes.BondedRoleConfig{
		reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR: {
			RoleType:          reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR,
			MinBond:           "999999",
			MinTrustLevel:     "TRUST_LEVEL_NEW",
			DemotionCooldown:  1,
			DemotionThreshold: "1",
		},
	}

	params := types.DefaultParams()
	params.MinCuratorBond = math.NewInt(1234)
	require.NoError(t, f.keeper.SyncCuratorBondedRoleConfig(f.ctx, params))

	cfg := f.repKeeper.bondedRoleConfigs[reptypes.RoleType_ROLE_TYPE_COLLECT_CURATOR]
	require.Equal(t, "1234", cfg.MinBond)
	require.NotEqual(t, "TRUST_LEVEL_NEW", cfg.MinTrustLevel,
		"sync must have overwritten the stale trust level")
}
