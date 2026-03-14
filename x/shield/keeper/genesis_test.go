package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

func TestGenesisDefaultRoundTrip(t *testing.T) {
	f := initFixture(t)

	// Export the default genesis (set by initFixture)
	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, exported.Validate())

	// Import into fresh keeper
	f2 := initFixtureEmpty(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, *exported))

	// Re-export and compare key fields
	reExported, err := f2.keeper.ExportGenesis(f2.ctx)
	require.NoError(t, err)
	require.Equal(t, len(exported.RegisteredOps), len(reExported.RegisteredOps))
	require.Equal(t, exported.Params.Enabled, reExported.Params.Enabled)
	require.Equal(t, exported.Params.MaxGasPerExec, reExported.Params.MaxGasPerExec)
}

func TestGenesisWithTLEKeySet(t *testing.T) {
	f := initFixture(t)

	ks := types.TLEKeySet{
		MasterPublicKey:      []byte("test_mpk"),
		ThresholdNumerator:   2,
		ThresholdDenominator: 3,
		ValidatorShares: []*types.TLEValidatorPublicShare{
			{ValidatorAddress: "val1", PublicShare: []byte("share1"), ShareIndex: 0},
		},
	}
	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, ks))

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Equal(t, []byte("test_mpk"), exported.TleKeySet.MasterPublicKey)
	require.Len(t, exported.TleKeySet.ValidatorShares, 1)

	// Import and verify
	f2 := initFixtureEmpty(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, *exported))

	got, found := f2.keeper.GetTLEKeySetVal(f2.ctx)
	require.True(t, found)
	require.Equal(t, []byte("test_mpk"), got.MasterPublicKey)
}

func TestGenesisWithDKGState(t *testing.T) {
	f := initFixture(t)

	dkgState := types.DKGState{
		Round:                1,
		Phase:                types.DKGPhase_DKG_PHASE_CONTRIBUTING,
		ExpectedValidators:   []string{"val1", "val2"},
		ThresholdNumerator:   2,
		ThresholdDenominator: 3,
	}
	require.NoError(t, f.keeper.SetDKGStateVal(f.ctx, dkgState))

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), exported.DkgState.Round)
	require.Equal(t, types.DKGPhase_DKG_PHASE_CONTRIBUTING, exported.DkgState.Phase)

	f2 := initFixtureEmpty(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, *exported))

	got, found := f2.keeper.GetDKGStateVal(f2.ctx)
	require.True(t, found)
	require.Equal(t, uint64(1), got.Round)
}

func TestGenesisWithRateLimits(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 3}))
	f.keeper.CheckAndIncrementRateLimit(f.ctx, "id1", 100)
	f.keeper.CheckAndIncrementRateLimit(f.ctx, "id1", 100)

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(exported.IdentityRateLimits), 1)

	f2 := initFixtureEmpty(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, *exported))

	// Verify rate limit count was imported (need to set epoch to match)
	require.NoError(t, f2.keeper.SetShieldEpochStateVal(f2.ctx, types.ShieldEpochState{CurrentEpoch: 3}))
	count := f2.keeper.GetIdentityRateLimitCount(f2.ctx, "id1")
	require.Equal(t, uint64(2), count)
}

func TestGenesisWithDayFundings(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.SetDayFunding(f.ctx, 1, math.NewInt(500)))
	require.NoError(t, f.keeper.SetDayFunding(f.ctx, 2, math.NewInt(750)))

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, exported.DayFundings, 2)

	f2 := initFixtureEmpty(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, *exported))

	require.Equal(t, math.NewInt(500), f2.keeper.GetDayFunding(f2.ctx, 1))
	require.Equal(t, math.NewInt(750), f2.keeper.GetDayFunding(f2.ctx, 2))
}

func TestGenesisWithDecryptionShares(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{
		Epoch: 1, Validator: "val1", Share: []byte("share_data"),
	}))

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, exported.DecryptionShares, 1)

	f2 := initFixtureEmpty(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, *exported))

	got, found := f2.keeper.GetDecryptionShare(f2.ctx, 1, "val1")
	require.True(t, found)
	require.Equal(t, []byte("share_data"), got.Share)
}

func TestGenesisWithTLEMissCounters(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.SetTLEMissCount(f.ctx, "val1", 5))
	require.NoError(t, f.keeper.SetTLEMissCount(f.ctx, "val2", 10))

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, exported.TleMissCounters, 2)

	f2 := initFixtureEmpty(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, *exported))

	require.Equal(t, uint64(5), f2.keeper.GetTLEMissCount(f2.ctx, "val1"))
	require.Equal(t, uint64(10), f2.keeper.GetTLEMissCount(f2.ctx, "val2"))
}

func TestGenesisEmptyState(t *testing.T) {
	f := initFixtureEmpty(t)

	// Init with truly empty genesis (only default params and ops)
	gen := types.DefaultGenesis()
	require.NoError(t, f.keeper.InitGenesis(f.ctx, *gen))

	// Export should work cleanly
	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, exported)
	require.NoError(t, exported.Validate())
}

func TestGenesisWithPendingOps(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{
		Id: 1, TargetEpoch: 5, EncryptedPayload: []byte("data"),
	}))

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, exported.PendingOps, 1)

	f2 := initFixtureEmpty(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, *exported))

	ops := f2.keeper.GetPendingOpsForEpoch(f2.ctx, 5)
	require.Len(t, ops, 1)
	require.Equal(t, []byte("data"), ops[0].EncryptedPayload)
}
