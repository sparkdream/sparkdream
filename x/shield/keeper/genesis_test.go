package keeper_test

import (
	"encoding/hex"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4/util/random"

	"sparkdream/tools/tle"
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

	secret := tle.Suite.G1().Scalar().Pick(random.New())
	pubG2, err := tle.Suite.G2().Point().Mul(secret, nil).MarshalBinary()
	require.NoError(t, err)
	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, types.TLEKeySet{
		MasterPublicKey:      tle.MasterPublicKeyG1(secret),
		MasterPublicKeyG2:    tle.MasterPublicKeyG2(secret),
		ThresholdNumerator:   1,
		ThresholdDenominator: 1,
		ValidatorShares:      []*types.TLEValidatorPublicShare{{ValidatorAddress: "val1", PublicShareG2: pubG2, ShareIndex: 1}},
	}))
	share := tle.DecryptionShare(secret, 1)
	require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{
		Epoch: 1, Validator: "val1", Share: share,
	}))

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, exported.DecryptionShares, 1)

	f2 := initFixtureEmpty(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, *exported))

	got, found := f2.keeper.GetDecryptionShare(f2.ctx, 1, "val1")
	require.True(t, found)
	require.Equal(t, share, got.Share)

	// SHD-16: a share that fails the pairing check (or comes from a
	// validator outside the key set) is refused on import.
	bad := *exported
	bad.DecryptionShares = []types.ShieldDecryptionShare{{Epoch: 1, Validator: "val1", Share: tle.DecryptionShare(secret, 2)}}
	f3 := initFixtureEmpty(t)
	require.ErrorIs(t, f3.keeper.InitGenesis(f3.ctx, bad), types.ErrInvalidDecryptionShare)
	bad.DecryptionShares = []types.ShieldDecryptionShare{{Epoch: 1, Validator: "val9", Share: share}}
	f4 := initFixtureEmpty(t)
	require.ErrorIs(t, f4.keeper.InitGenesis(f4.ctx, bad), types.ErrInvalidDecryptionShare)
}

// TestGenesisTLELivenessAndFeeLedgerRoundTrip (SHD-13): the TLE participation
// ring, the judged-epoch counter and the epoch fee ledger survive an
// export/import, and the pending-op sequence is restored in one write.
func TestGenesisTLELivenessAndFeeLedgerRoundTrip(t *testing.T) {
	f := initFixture(t)
	require.NoError(t, f.keeper.TLEEpochParticipation.Set(f.ctx, collections.Join("val1", uint64(3)), false))
	require.NoError(t, f.keeper.TLEEpochParticipation.Set(f.ctx, collections.Join("val2", uint64(3)), true))
	require.NoError(t, f.keeper.TLEJudgedEpochs.Set(f.ctx, 42))
	feeParams := types.DefaultParams()
	feeParams.MaxFeesPerEpoch = math.NewInt(100_000)
	rl := make([]byte, 32)
	rl[0] = 0x0a
	require.NoError(t, f.keeper.ChargeEpochFee(f.ctx, math.NewInt(1234), feeParams, &types.MsgShieldedExec{RateLimitNullifier: rl}))
	require.NoError(t, f.keeper.NextPendingOpId.Set(f.ctx, 1_000_000))

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, exported.TleParticipation, 2)

	f2 := initFixtureEmpty(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, *exported))
	missed, err := f2.keeper.TLEEpochParticipation.Get(f2.ctx, collections.Join("val1", uint64(3)))
	require.NoError(t, err)
	require.False(t, missed)
	part, err := f2.keeper.TLEEpochParticipation.Get(f2.ctx, collections.Join("val2", uint64(3)))
	require.NoError(t, err)
	require.True(t, part)
	judged, err := f2.keeper.TLEJudgedEpochs.Get(f2.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(42), judged)
	require.Equal(t, math.NewInt(1234), f2.keeper.EpochFeesPaid(f2.ctx))
	require.Len(t, exported.IdentityEpochFees, 1)
	idPaid, err := f2.keeper.IdentityEpochFeesPaid(f2.ctx, hex.EncodeToString(rl))
	require.NoError(t, err)
	require.Equal(t, math.NewInt(1234), idPaid)
	next, err := f2.keeper.NextPendingOpId.Peek(f2.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1_000_000), next)

	again, err := f2.keeper.ExportGenesis(f2.ctx)
	require.NoError(t, err)
	require.Equal(t, exported, again, "export-import-export is a fixed point")
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

// InitGenesis creates the public anonymous submitter's account: clients sign
// with its account number, which a just-created account wouldn't have yet.
// An existing account (e.g. from an exported genesis) is left as it is.
func TestInitGenesisCreatesPublicSubmitterAccount(t *testing.T) {
	ak := mockAccountKeeper{accounts: map[string]sdk.AccountI{}}
	f := initFixtureEmptyWithAccounts(t, ak)
	submitter := types.PublicSubmitterAddress()

	require.NoError(t, f.keeper.InitGenesis(f.ctx, *types.DefaultGenesis()))
	acc := ak.GetAccount(f.ctx, submitter)
	require.NotNil(t, acc)

	require.NoError(t, acc.SetSequence(7))
	ak.SetAccount(f.ctx, acc)
	require.NoError(t, f.keeper.InitGenesis(f.ctx, *types.DefaultGenesis()))
	require.Equal(t, uint64(7), ak.GetAccount(f.ctx, submitter).GetSequence(), "an existing account is kept")
}

// TestGenesisExportKeepsDeregistrations: the export lists exactly the stored
// registrations, so an op governance deregistered stays gone across an
// export/import and no op is listed twice.
func TestGenesisExportKeepsDeregistrations(t *testing.T) {
	f := initFixture(t)
	defaults := types.DefaultGenesis().RegisteredOps
	require.NotEmpty(t, defaults)
	removed := defaults[0].MessageTypeUrl
	require.NoError(t, f.keeper.DeleteShieldedOp(f.ctx, removed))

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, exported.Validate())
	require.Len(t, exported.RegisteredOps, len(defaults)-1)
	for _, reg := range exported.RegisteredOps {
		require.NotEqual(t, removed, reg.MessageTypeUrl, "deregistered op resurrected by export")
	}

	f2 := initFixtureEmpty(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, *exported))
	_, found := f2.keeper.GetShieldedOp(f2.ctx, removed)
	require.False(t, found)
}

func TestGenesisValidateRejectsDuplicateOps(t *testing.T) {
	gs := types.DefaultGenesis()
	gs.RegisteredOps = append(gs.RegisteredOps, gs.RegisteredOps[0])
	require.ErrorContains(t, gs.Validate(), "duplicate registered op")
}
