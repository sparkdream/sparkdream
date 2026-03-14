package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/keeper"
	module "sparkdream/x/shield/module"
	"sparkdream/x/shield/types"
)

// --- Mock Keepers ---

type mockAccountKeeper struct {
	GetModuleAddressFn func(moduleName string) sdk.AccAddress
}

func (m mockAccountKeeper) GetModuleAddress(moduleName string) sdk.AccAddress {
	if m.GetModuleAddressFn != nil {
		return m.GetModuleAddressFn(moduleName)
	}
	return authtypes.NewModuleAddress(moduleName)
}

func (m mockAccountKeeper) GetModuleAccount(ctx context.Context, moduleName string) sdk.ModuleAccountI {
	return nil
}

func (m mockAccountKeeper) GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI {
	return nil
}

type mockBankKeeper struct{}

func (m mockBankKeeper) GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewCoin("uspark", math.NewInt(1000000000))
}

func (m mockBankKeeper) SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1000000000)))
}

func (m mockBankKeeper) SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error {
	return nil
}

type mockStakingKeeper struct {
	validators []stakingtypes.Validator
}

func (m mockStakingKeeper) GetValidator(_ context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error) {
	for _, v := range m.validators {
		if v.GetOperator() == addr.String() {
			return v, nil
		}
	}
	return stakingtypes.Validator{}, fmt.Errorf("validator not found")
}

func (m mockStakingKeeper) GetValidatorByConsAddr(_ context.Context, _ sdk.ConsAddress) (stakingtypes.Validator, error) {
	return stakingtypes.Validator{}, fmt.Errorf("not implemented")
}

func (m mockStakingKeeper) GetBondedValidatorsByPower(_ context.Context) ([]stakingtypes.Validator, error) {
	return m.validators, nil
}

// --- Test Fixture ---

type fixture struct {
	ctx          sdk.Context
	keeper       keeper.Keeper
	addressCodec address.Codec
}

func initFixture(t *testing.T) *fixture {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	key := storetypes.NewKVStoreKey(types.StoreKey)
	storeService := runtime.NewKVStoreService(key)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("transient_test"))
	ctx := testCtx.Ctx

	addrCodec := addresscodec.NewBech32Codec("sprkdrm")
	authority := authtypes.NewModuleAddress("gov")

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addrCodec,
		authority,
		mockAccountKeeper{},
		mockBankKeeper{},
	)

	// Wire mock staking keeper with 5 test validators (MinTleValidators=5 in production)
	mockSK := mockStakingKeeper{
		validators: []stakingtypes.Validator{
			{OperatorAddress: "sprkdrmvaloper1aaaaa"},
			{OperatorAddress: "sprkdrmvaloper1bbbbb"},
			{OperatorAddress: "sprkdrmvaloper1ccccc"},
			{OperatorAddress: "sprkdrmvaloper1ddddd"},
			{OperatorAddress: "sprkdrmvaloper1eeeee"},
		},
	}
	k.SetStakingKeeper(mockSK)

	// Initialize genesis with defaults
	err := k.InitGenesis(ctx, *types.DefaultGenesis())
	require.NoError(t, err)

	return &fixture{
		ctx:          ctx,
		keeper:       k,
		addressCodec: addrCodec,
	}
}

// --- Nullifier Tests ---

func TestNullifierUsed(t *testing.T) {
	f := initFixture(t)

	t.Run("fresh nullifier not used", func(t *testing.T) {
		used := f.keeper.IsNullifierUsed(f.ctx, 1, 100, "abc123")
		require.False(t, used)
	})

	t.Run("record and check", func(t *testing.T) {
		err := f.keeper.RecordNullifier(f.ctx, 1, 100, "abc123", 42)
		require.NoError(t, err)

		used := f.keeper.IsNullifierUsed(f.ctx, 1, 100, "abc123")
		require.True(t, used)
	})

	t.Run("different domain not affected", func(t *testing.T) {
		used := f.keeper.IsNullifierUsed(f.ctx, 2, 100, "abc123")
		require.False(t, used)
	})

	t.Run("different scope not affected", func(t *testing.T) {
		used := f.keeper.IsNullifierUsed(f.ctx, 1, 200, "abc123")
		require.False(t, used)
	})

	t.Run("get used nullifier", func(t *testing.T) {
		n, found := f.keeper.GetUsedNullifier(f.ctx, 1, 100, "abc123")
		require.True(t, found)
		require.Equal(t, uint32(1), n.Domain)
		require.Equal(t, uint64(100), n.Scope)
		require.Equal(t, "abc123", n.NullifierHex)
		require.Equal(t, int64(42), n.UsedAtHeight)
	})

	t.Run("get nonexistent returns not found", func(t *testing.T) {
		_, found := f.keeper.GetUsedNullifier(f.ctx, 99, 0, "nonexistent")
		require.False(t, found)
	})
}

func TestPendingNullifiers(t *testing.T) {
	f := initFixture(t)

	t.Run("not pending initially", func(t *testing.T) {
		require.False(t, f.keeper.IsPendingNullifier(f.ctx, "pend1"))
	})

	t.Run("record and check", func(t *testing.T) {
		err := f.keeper.RecordPendingNullifier(f.ctx, "pend1")
		require.NoError(t, err)
		require.True(t, f.keeper.IsPendingNullifier(f.ctx, "pend1"))
	})

	t.Run("delete and check", func(t *testing.T) {
		err := f.keeper.DeletePendingNullifier(f.ctx, "pend1")
		require.NoError(t, err)
		require.False(t, f.keeper.IsPendingNullifier(f.ctx, "pend1"))
	})
}

func TestPruneEpochScopedNullifiers(t *testing.T) {
	f := initFixture(t)

	// Record nullifiers for epoch-scoped domain 1 (blog posts) and global domain 41 (rep challenges)
	// Domain 1 is EPOCH scoped, domain 41 is GLOBAL scoped
	require.NoError(t, f.keeper.RecordNullifier(f.ctx, 1, 5, "old_epoch", 10))
	require.NoError(t, f.keeper.RecordNullifier(f.ctx, 1, 10, "current_epoch", 20))
	require.NoError(t, f.keeper.RecordNullifier(f.ctx, 41, 0, "global_null", 15))

	// Prune epoch-scoped nullifiers older than epoch 10
	err := f.keeper.PruneEpochScopedNullifiers(f.ctx, 10)
	require.NoError(t, err)

	// Old epoch-scoped nullifier should be pruned
	require.False(t, f.keeper.IsNullifierUsed(f.ctx, 1, 5, "old_epoch"))
	// Current epoch-scoped nullifier should remain
	require.True(t, f.keeper.IsNullifierUsed(f.ctx, 1, 10, "current_epoch"))
	// Global-scoped nullifier should remain (not pruned)
	require.True(t, f.keeper.IsNullifierUsed(f.ctx, 41, 0, "global_null"))
}

// --- Rate Limit Tests ---

func TestRateLimit(t *testing.T) {
	f := initFixture(t)

	// Set epoch state so GetCurrentEpoch returns a known value
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{
		CurrentEpoch: 1,
	}))

	t.Run("first use within limit", func(t *testing.T) {
		ok := f.keeper.CheckAndIncrementRateLimit(f.ctx, "identity1", 3)
		require.True(t, ok)
	})

	t.Run("second and third use within limit", func(t *testing.T) {
		require.True(t, f.keeper.CheckAndIncrementRateLimit(f.ctx, "identity1", 3))
		require.True(t, f.keeper.CheckAndIncrementRateLimit(f.ctx, "identity1", 3))
	})

	t.Run("fourth use exceeds limit", func(t *testing.T) {
		ok := f.keeper.CheckAndIncrementRateLimit(f.ctx, "identity1", 3)
		require.False(t, ok)
	})

	t.Run("different identity is independent", func(t *testing.T) {
		ok := f.keeper.CheckAndIncrementRateLimit(f.ctx, "identity2", 3)
		require.True(t, ok)
	})

	t.Run("count tracking", func(t *testing.T) {
		count := f.keeper.GetIdentityRateLimitCount(f.ctx, "identity1")
		require.Equal(t, uint64(3), count)

		count = f.keeper.GetIdentityRateLimitCount(f.ctx, "identity2")
		require.Equal(t, uint64(1), count)

		count = f.keeper.GetIdentityRateLimitCount(f.ctx, "unknown")
		require.Equal(t, uint64(0), count)
	})
}

func TestPruneIdentityRateLimits(t *testing.T) {
	f := initFixture(t)

	// Set up rate limits at different epochs
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 1}))
	f.keeper.CheckAndIncrementRateLimit(f.ctx, "id1", 100)

	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 5}))
	f.keeper.CheckAndIncrementRateLimit(f.ctx, "id2", 100)

	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 10}))
	f.keeper.CheckAndIncrementRateLimit(f.ctx, "id3", 100)

	// Prune epochs < 5
	err := f.keeper.PruneIdentityRateLimits(f.ctx, 5)
	require.NoError(t, err)

	// Epoch 1 should be pruned, epoch 5 and 10 should remain
	// Switch back to check specific epochs
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 1}))
	require.Equal(t, uint64(0), f.keeper.GetIdentityRateLimitCount(f.ctx, "id1"))

	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 5}))
	require.Equal(t, uint64(1), f.keeper.GetIdentityRateLimitCount(f.ctx, "id2"))

	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 10}))
	require.Equal(t, uint64(1), f.keeper.GetIdentityRateLimitCount(f.ctx, "id3"))
}

// --- Registration Tests ---

func TestShieldedOpRegistration(t *testing.T) {
	f := initFixture(t)

	t.Run("get registered op", func(t *testing.T) {
		reg, found := f.keeper.GetShieldedOp(f.ctx, "/sparkdream.blog.v1.MsgCreatePost")
		require.True(t, found)
		require.Equal(t, types.ProofDomain_PROOF_DOMAIN_TRUST_TREE, reg.ProofDomain)
		require.Equal(t, uint32(1), reg.MinTrustLevel)
		require.True(t, reg.Active)
	})

	t.Run("get unregistered op", func(t *testing.T) {
		_, found := f.keeper.GetShieldedOp(f.ctx, "/sparkdream.unknown.v1.MsgFoo")
		require.False(t, found)
	})

	t.Run("register new op", func(t *testing.T) {
		newReg := types.ShieldedOpRegistration{
			MessageTypeUrl:     "/sparkdream.test.v1.MsgTest",
			ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      2,
			NullifierDomain:    99,
			NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_GLOBAL,
			Active:             true,
			BatchMode:          types.ShieldBatchMode_SHIELD_BATCH_MODE_IMMEDIATE_ONLY,
		}
		err := f.keeper.SetShieldedOp(f.ctx, newReg)
		require.NoError(t, err)

		got, found := f.keeper.GetShieldedOp(f.ctx, "/sparkdream.test.v1.MsgTest")
		require.True(t, found)
		require.Equal(t, uint32(2), got.MinTrustLevel)
		require.Equal(t, uint32(99), got.NullifierDomain)
	})

	t.Run("delete op", func(t *testing.T) {
		err := f.keeper.DeleteShieldedOp(f.ctx, "/sparkdream.test.v1.MsgTest")
		require.NoError(t, err)

		_, found := f.keeper.GetShieldedOp(f.ctx, "/sparkdream.test.v1.MsgTest")
		require.False(t, found)
	})

	t.Run("iterate ops", func(t *testing.T) {
		count := 0
		err := f.keeper.IterateShieldedOps(f.ctx, func(_ string, _ types.ShieldedOpRegistration) bool {
			count++
			return false
		})
		require.NoError(t, err)
		// Default genesis registers 12 ops (blog:3, forum:3, collect:3, rep:1, commons:2)
		require.Equal(t, 12, count)
	})
}

// --- Epoch Tests ---

func TestEpochState(t *testing.T) {
	f := initFixture(t)

	t.Run("default epoch is 0", func(t *testing.T) {
		epoch := f.keeper.GetCurrentEpoch(f.ctx)
		require.Equal(t, uint64(0), epoch)
	})

	t.Run("set and get epoch state", func(t *testing.T) {
		state := types.ShieldEpochState{
			CurrentEpoch:     7,
			EpochStartHeight: 350,
		}
		require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, state))

		got, found := f.keeper.GetShieldEpochStateVal(f.ctx)
		require.True(t, found)
		require.Equal(t, uint64(7), got.CurrentEpoch)
		require.Equal(t, int64(350), got.EpochStartHeight)

		require.Equal(t, uint64(7), f.keeper.GetCurrentEpoch(f.ctx))
	})
}

// --- Genesis Tests ---

func TestGenesisImportExport(t *testing.T) {
	f := initFixture(t)

	// Add some state beyond defaults
	require.NoError(t, f.keeper.RecordNullifier(f.ctx, 1, 5, "nullhex1", 100))
	require.NoError(t, f.keeper.RecordNullifier(f.ctx, 41, 0, "nullhex2", 200))
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{
		CurrentEpoch:     3,
		EpochStartHeight: 150,
	}))
	require.NoError(t, f.keeper.RecordPendingNullifier(f.ctx, "pending1"))

	// Export
	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, exported)

	// Validate exported state
	require.NoError(t, exported.Validate())
	require.GreaterOrEqual(t, len(exported.RegisteredOps), 11) // Default ops
	require.Len(t, exported.UsedNullifiers, 2)
	require.Equal(t, uint64(3), exported.ShieldEpochState.CurrentEpoch)
	require.Len(t, exported.PendingNullifiers, 1)

	// Import into a fresh keeper
	f2 := initFixtureEmpty(t)
	err = f2.keeper.InitGenesis(f2.ctx, *exported)
	require.NoError(t, err)

	// Verify state was imported
	require.True(t, f2.keeper.IsNullifierUsed(f2.ctx, 1, 5, "nullhex1"))
	require.True(t, f2.keeper.IsNullifierUsed(f2.ctx, 41, 0, "nullhex2"))
	require.Equal(t, uint64(3), f2.keeper.GetCurrentEpoch(f2.ctx))
	require.True(t, f2.keeper.IsPendingNullifier(f2.ctx, "pending1"))

	reg, found := f2.keeper.GetShieldedOp(f2.ctx, "/sparkdream.blog.v1.MsgCreatePost")
	require.True(t, found)
	require.True(t, reg.Active)
}

// initFixtureEmpty creates a keeper without initializing genesis
func initFixtureEmpty(t *testing.T) *fixture {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	key := storetypes.NewKVStoreKey(types.StoreKey)
	storeService := runtime.NewKVStoreService(key)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("transient_test"))
	ctx := testCtx.Ctx

	addrCodec := addresscodec.NewBech32Codec("sprkdrm")
	authority := authtypes.NewModuleAddress("gov")

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addrCodec,
		authority,
		mockAccountKeeper{},
		mockBankKeeper{},
	)

	return &fixture{
		ctx:          ctx,
		keeper:       k,
		addressCodec: addrCodec,
	}
}

// --- ShieldAware Registration Tests ---

func TestShieldAwareModule(t *testing.T) {
	f := initFixture(t)

	// Create a mock ShieldAware implementation
	mockSA := &mockShieldAware{compatible: true}

	f.keeper.RegisterShieldAwareModule("/sparkdream.test.v1.", mockSA)

	// Verify we can't test getShieldAware directly (unexported),
	// but we can verify the keeper builds and registration doesn't panic
	require.NotPanics(t, func() {
		f.keeper.RegisterShieldAwareModule("/sparkdream.other.v1.", mockSA)
	})
}

type mockShieldAware struct {
	compatible bool
}

func (m *mockShieldAware) IsShieldCompatible(_ context.Context, _ sdk.Msg) bool {
	return m.compatible
}

// --- Pending Ops Tests ---

func TestPendingOps(t *testing.T) {
	f := initFixture(t)

	t.Run("get pending op count starts at 0", func(t *testing.T) {
		count := f.keeper.GetPendingOpCountVal(f.ctx)
		require.Equal(t, uint64(0), count)
	})

	t.Run("set and get pending op", func(t *testing.T) {
		op := types.PendingShieldedOp{
			Id:                1,
			TargetEpoch:       5,
			Nullifier:         []byte("null1"),
			MerkleRoot:        []byte("root1"),
			EncryptedPayload:  []byte("encrypted_data"),
			SubmittedAtHeight: 100,
			SubmittedAtEpoch:  5,
		}
		err := f.keeper.SetPendingOp(f.ctx, op)
		require.NoError(t, err)

		// Verify via epoch lookup
		ops := f.keeper.GetPendingOpsForEpoch(f.ctx, 5)
		require.Len(t, ops, 1)
		require.Equal(t, uint64(5), ops[0].TargetEpoch)
		require.Equal(t, []byte("encrypted_data"), ops[0].EncryptedPayload)

		require.Equal(t, uint64(1), f.keeper.GetPendingOpCountVal(f.ctx))
	})

	t.Run("delete pending op", func(t *testing.T) {
		err := f.keeper.DeletePendingOp(f.ctx, 1)
		require.NoError(t, err)

		require.Equal(t, uint64(0), f.keeper.GetPendingOpCountVal(f.ctx))
	})
}

// --- Params Tests ---

func TestParamsValidation(t *testing.T) {
	t.Run("default params are valid", func(t *testing.T) {
		p := types.DefaultParams()
		require.NoError(t, p.Validate())
	})

	t.Run("zero max gas per exec is invalid", func(t *testing.T) {
		p := types.DefaultParams()
		p.MaxGasPerExec = 0
		require.Error(t, p.Validate())
	})

	t.Run("zero max execs per identity is invalid", func(t *testing.T) {
		p := types.DefaultParams()
		p.MaxExecsPerIdentityPerEpoch = 0
		require.Error(t, p.Validate())
	})

	t.Run("miss tolerance exceeds window is invalid", func(t *testing.T) {
		p := types.DefaultParams()
		p.TleMissTolerance = 200
		p.TleMissWindow = 100
		require.Error(t, p.Validate())
	})

	t.Run("negative funding per day is invalid", func(t *testing.T) {
		p := types.DefaultParams()
		p.MaxFundingPerDay = math.NewInt(-1)
		require.Error(t, p.Validate())
	})

	t.Run("negative jail duration is invalid", func(t *testing.T) {
		p := types.DefaultParams()
		p.TleJailDuration = -1
		require.Error(t, p.Validate())
	})
}

// --- TLE Key Set Tests ---

func TestTLEKeySet(t *testing.T) {
	f := initFixture(t)

	t.Run("not found initially", func(t *testing.T) {
		_, found := f.keeper.GetTLEKeySetVal(f.ctx)
		require.False(t, found)
	})

	t.Run("set and get", func(t *testing.T) {
		ks := types.TLEKeySet{
			MasterPublicKey: []byte("master_pk"),
			ValidatorShares: []*types.TLEValidatorPublicShare{
				{
					ValidatorAddress: "val1",
					PublicShare:      []byte("share1"),
					ShareIndex:       0,
				},
			},
		}
		require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, ks))

		got, found := f.keeper.GetTLEKeySetVal(f.ctx)
		require.True(t, found)
		require.Equal(t, []byte("master_pk"), got.MasterPublicKey)
		require.Len(t, got.ValidatorShares, 1)
	})
}

// --- TLE Miss Counter Tests ---

func TestTLEMissCounters(t *testing.T) {
	f := initFixture(t)

	t.Run("initial count is 0", func(t *testing.T) {
		count := f.keeper.GetTLEMissCount(f.ctx, "val1")
		require.Equal(t, uint64(0), count)
	})

	t.Run("set and get", func(t *testing.T) {
		require.NoError(t, f.keeper.SetTLEMissCount(f.ctx, "val1", 5))
		count := f.keeper.GetTLEMissCount(f.ctx, "val1")
		require.Equal(t, uint64(5), count)
	})

	t.Run("increment", func(t *testing.T) {
		newCount := f.keeper.IncrementTLEMissCount(f.ctx, "val1")
		require.Equal(t, uint64(6), newCount)
		count := f.keeper.GetTLEMissCount(f.ctx, "val1")
		require.Equal(t, uint64(6), count)
	})

	t.Run("reset", func(t *testing.T) {
		require.NoError(t, f.keeper.ResetTLEMissCount(f.ctx, "val1"))
		count := f.keeper.GetTLEMissCount(f.ctx, "val1")
		require.Equal(t, uint64(0), count)
	})
}

// --- Day Funding Tests ---

func TestDayFunding(t *testing.T) {
	f := initFixture(t)

	t.Run("default funding is zero", func(t *testing.T) {
		amount := f.keeper.GetDayFunding(f.ctx, 1)
		require.True(t, amount.IsZero())
	})

	t.Run("set funding", func(t *testing.T) {
		err := f.keeper.SetDayFunding(f.ctx, 1, math.NewInt(100000))
		require.NoError(t, err)

		amount := f.keeper.GetDayFunding(f.ctx, 1)
		require.Equal(t, math.NewInt(100000), amount)
	})

	t.Run("overwrite funding", func(t *testing.T) {
		err := f.keeper.SetDayFunding(f.ctx, 1, math.NewInt(150000))
		require.NoError(t, err)

		amount := f.keeper.GetDayFunding(f.ctx, 1)
		require.Equal(t, math.NewInt(150000), amount)
	})

	t.Run("different days independent", func(t *testing.T) {
		err := f.keeper.SetDayFunding(f.ctx, 2, math.NewInt(200000))
		require.NoError(t, err)

		require.Equal(t, math.NewInt(150000), f.keeper.GetDayFunding(f.ctx, 1))
		require.Equal(t, math.NewInt(200000), f.keeper.GetDayFunding(f.ctx, 2))
	})
}

// --- Decryption State Tests ---

func TestPruneDecryptionState(t *testing.T) {
	f := initFixture(t)

	// Add decryption keys for epochs 1, 5, 10
	require.NoError(t, f.keeper.SetShieldEpochDecryptionKey(f.ctx, types.ShieldEpochDecryptionKey{
		Epoch:         1,
		DecryptionKey: []byte("key1"),
	}))
	require.NoError(t, f.keeper.SetShieldEpochDecryptionKey(f.ctx, types.ShieldEpochDecryptionKey{
		Epoch:         5,
		DecryptionKey: []byte("key5"),
	}))
	require.NoError(t, f.keeper.SetShieldEpochDecryptionKey(f.ctx, types.ShieldEpochDecryptionKey{
		Epoch:         10,
		DecryptionKey: []byte("key10"),
	}))

	// Prune epochs < 5
	err := f.keeper.PruneDecryptionState(f.ctx, 5)
	require.NoError(t, err)

	// Epoch 1 should be pruned
	_, found := f.keeper.GetShieldEpochDecryptionKeyVal(f.ctx, 1)
	require.False(t, found)

	// Epochs 5 and 10 should remain
	_, found = f.keeper.GetShieldEpochDecryptionKeyVal(f.ctx, 5)
	require.True(t, found)
	_, found = f.keeper.GetShieldEpochDecryptionKeyVal(f.ctx, 10)
	require.True(t, found)
}

// --- Default Genesis Ops Tests ---

func TestDefaultGenesisOps(t *testing.T) {
	genesis := types.DefaultGenesis()
	require.NotNil(t, genesis)

	// Verify default ops cover all expected modules
	opsByModule := make(map[string]int)
	for _, op := range genesis.RegisteredOps {
		if len(op.MessageTypeUrl) > 0 {
			// Extract module prefix (e.g., "/sparkdream.blog.v1." → "blog")
			parts := splitTypeURL(op.MessageTypeUrl)
			if len(parts) >= 2 {
				opsByModule[parts[1]]++
			}
		}
	}

	require.Equal(t, 3, opsByModule["blog"], "blog should have 3 ops")
	require.Equal(t, 3, opsByModule["forum"], "forum should have 3 ops")
	require.Equal(t, 3, opsByModule["collect"], "collect should have 3 ops")
	require.Equal(t, 1, opsByModule["rep"], "rep should have 1 op")
	require.Equal(t, 2, opsByModule["commons"], "commons should have 2 ops")

	// Verify domain uniqueness
	domains := make(map[uint32]string)
	for _, op := range genesis.RegisteredOps {
		if existing, ok := domains[op.NullifierDomain]; ok {
			t.Errorf("duplicate nullifier domain %d: %s and %s", op.NullifierDomain, existing, op.MessageTypeUrl)
		}
		domains[op.NullifierDomain] = op.MessageTypeUrl
	}
}

// splitTypeURL splits a type URL like "/sparkdream.blog.v1.MsgCreatePost" into parts
func splitTypeURL(typeURL string) []string {
	// Remove leading "/"
	if len(typeURL) > 0 && typeURL[0] == '/' {
		typeURL = typeURL[1:]
	}
	parts := make([]string, 0)
	start := 0
	for i, c := range typeURL {
		if c == '.' {
			parts = append(parts, typeURL[start:i])
			start = i + 1
		}
	}
	if start < len(typeURL) {
		parts = append(parts, typeURL[start:])
	}
	return parts
}
