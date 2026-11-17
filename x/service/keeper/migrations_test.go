package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

// The v1 -> v2 migration writes the storage-arweave service type on a chain
// that started without it, and leaves a type governance already wrote alone.
func TestMigrateV1ToV2SeedsStorageArweave(t *testing.T) {
	f := initFixture(t)
	_, found := f.keeper.GetServiceTypeConfig(f.ctx, types.StorageArweaveServiceType)
	require.False(t, found)
	_, ok := f.keeper.UnbondingPeriodBlocks(f.ctx, types.StorageArweaveServiceType)
	require.False(t, ok)

	require.NoError(t, f.keeper.MigrateV1ToV2(f.ctx))
	cfg, found := f.keeper.GetServiceTypeConfig(f.ctx, types.StorageArweaveServiceType)
	require.True(t, found)
	require.True(t, cfg.Enabled)
	require.Equal(t, types.ReportTimeoutAction_REPORT_TIMEOUT_ACTION_ESCALATE, cfg.ReportTimeoutAction)
	require.Zero(t, cfg.AttestationQuorum)
	require.Zero(t, cfg.CheckpointMaxLagBlocks)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	period, ok := f.keeper.UnbondingPeriodBlocks(f.ctx, types.StorageArweaveServiceType)
	require.True(t, ok)
	require.Equal(t, cfg.UnbondingPeriodBlocks, period)
	require.Greater(t, period, params.DefaultUnbondingPeriodBlocks, "storage operators stay bonded longer than the default")

	// A type with no override reads the module default.
	cfg.ServiceType = "storage-zenith"
	cfg.UnbondingPeriodBlocks = 0
	require.NoError(t, f.keeper.SetServiceTypeConfig(f.ctx, cfg))
	period, ok = f.keeper.UnbondingPeriodBlocks(f.ctx, "storage-zenith")
	require.True(t, ok)
	require.Equal(t, params.DefaultUnbondingPeriodBlocks, period)

	// Run again after governance changed the type: nothing is overwritten.
	cfg.ServiceType = types.StorageArweaveServiceType
	cfg.Description = "changed by governance"
	require.NoError(t, f.keeper.SetServiceTypeConfig(f.ctx, cfg))
	require.NoError(t, f.keeper.MigrateV1ToV2(f.ctx))
	got, _ := f.keeper.GetServiceTypeConfig(f.ctx, types.StorageArweaveServiceType)
	require.Equal(t, "changed by governance", got.Description)
}

// The seeded genesis carries the type, and it validates.
func TestDefaultGenesisSeedsStorageArweave(t *testing.T) {
	g := types.DefaultGenesis()
	require.NoError(t, g.Validate())
	found := false
	for _, cfg := range g.ServiceTypes {
		if cfg.ServiceType == types.StorageArweaveServiceType {
			found = true
			require.Equal(t, g.Params.DefaultUnbondingPeriodBlocks*30/7, cfg.UnbondingPeriodBlocks)
		}
	}
	require.True(t, found)
}
