package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/types"
	reptypes "sparkdream/x/rep/types"
)

func TestSyncVerifierBondedRoleConfig_WritesThroughToRep(t *testing.T) {
	f := initFixture(t)

	params := types.DefaultParams()
	require.NoError(t, f.keeper.SyncVerifierBondedRoleConfig(f.ctx, params))

	cfg, ok := f.repKeeper.bondedRoleConfigs[reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER]
	require.True(t, ok, "rep mock must have received the verifier config")
	require.Equal(t, reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, cfg.RoleType)
	require.Equal(t, params.MinVerifierBond.String(), cfg.MinBond)
	require.Equal(t, uint64(0), cfg.MinRepTier,
		"federation verifier is trust-level-gated, not rep-tier-gated")
	require.Equal(t, int64(0), cfg.MinAgeBlocks,
		"federation verifier has no age gate")
	require.Equal(t, int64(params.VerifierDemotionCooldown.Seconds()), cfg.DemotionCooldown,
		"demotion cooldown must be converted from Duration to seconds")
	require.Equal(t, params.VerifierRecoveryThreshold.String(), cfg.DemotionThreshold)
}

func TestSyncVerifierBondedRoleConfig_TrustLevelIdToEnumName(t *testing.T) {
	f := initFixture(t)

	// Custom trust-level IDs exercise the uint32→enum-name mapping.
	cases := []struct {
		id   uint32
		want string
	}{
		{0, ""}, // zero → no gate, empty string
		{1, "TRUST_LEVEL_PROVISIONAL"},
		{2, "TRUST_LEVEL_ESTABLISHED"},
		{3, "TRUST_LEVEL_TRUSTED"},
		{4, "TRUST_LEVEL_CORE"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			params := types.DefaultParams()
			params.MinVerifierTrustLevel = tc.id
			require.NoError(t, f.keeper.SyncVerifierBondedRoleConfig(f.ctx, params))
			cfg := f.repKeeper.bondedRoleConfigs[reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER]
			require.Equal(t, tc.want, cfg.MinTrustLevel)
		})
	}
}

func TestSyncVerifierBondedRoleConfig_NilAmountsNormalizeToZero(t *testing.T) {
	f := initFixture(t)

	// Mostly-empty params to exercise the defensive nil-checks on
	// MinVerifierBond and VerifierRecoveryThreshold.
	params := types.Params{
		MinVerifierTrustLevel:    2,
		VerifierDemotionCooldown: 3600 * time.Second,
	}
	require.NoError(t, f.keeper.SyncVerifierBondedRoleConfig(f.ctx, params))

	cfg := f.repKeeper.bondedRoleConfigs[reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER]
	require.Equal(t, "0", cfg.MinBond)
	require.Equal(t, "0", cfg.DemotionThreshold)
	require.Equal(t, int64(3600), cfg.DemotionCooldown)
	require.Equal(t, "TRUST_LEVEL_ESTABLISHED", cfg.MinTrustLevel)
}

func TestSyncVerifierBondedRoleConfig_OverwritesExisting(t *testing.T) {
	f := initFixture(t)

	// Seed a stale config; sync should replace it.
	f.repKeeper.bondedRoleConfigs = map[reptypes.RoleType]reptypes.BondedRoleConfig{
		reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER: {
			RoleType:          reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER,
			MinBond:           "999999",
			MinTrustLevel:     "TRUST_LEVEL_NEW",
			DemotionCooldown:  1,
			DemotionThreshold: "1",
		},
	}

	params := types.DefaultParams()
	params.MinVerifierBond = math.NewInt(4321)
	require.NoError(t, f.keeper.SyncVerifierBondedRoleConfig(f.ctx, params))

	cfg := f.repKeeper.bondedRoleConfigs[reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER]
	require.Equal(t, "4321", cfg.MinBond)
	require.NotEqual(t, "TRUST_LEVEL_NEW", cfg.MinTrustLevel,
		"sync must have overwritten the stale trust level")
}

func TestSyncVerifierBondedRoleConfig_NoRepKeeperIsNoop(t *testing.T) {
	f := initFixture(t)

	// Clear rep wiring (standalone-construction simulation).
	f.keeper.SetRepKeeper(nil)
	require.NoError(t, f.keeper.SyncVerifierBondedRoleConfig(f.ctx, types.DefaultParams()))
	require.Empty(t, f.repKeeper.bondedRoleConfigs,
		"no write-through expected when rep keeper is unwired")
}
