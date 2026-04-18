package keeper_test

import (
	"context"
	"testing"
	"time"

	"cosmossdk.io/core/address"
	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/codec"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	codectestutil "github.com/cosmos/cosmos-sdk/codec/testutil"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/commons/keeper"
	module "sparkdream/x/commons/module"
	"sparkdream/x/commons/types"
)

type fixture struct {
	ctx          context.Context
	keeper       keeper.Keeper
	addressCodec address.Codec
}

func initFixture(t *testing.T) *fixture {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())

	// Define Keys
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	memStoreKey := storetypes.NewMemoryStoreKey("mem_commons")

	// Setup Store
	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(memStoreKey, storetypes.StoreTypeMemory, nil)
	require.NoError(t, stateStore.LoadLatestVersion())

	// Initialize Context with positive time
	ctx := sdk.NewContext(stateStore, cmtproto.Header{Time: time.Now()}, false, log.NewNopLogger())

	authority := authtypes.NewModuleAddress(types.GovModuleName)
	mockAuth := mockAuthKeeper{}

	storeService := runtime.NewKVStoreService(storeKey)

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
		mockAuth,
		nil, // Bank
		mockFutarchyKeeper{},
		nil, // Gov
		mockSplitKeeper{},
		mockUpgradeKeeper{},
	)

	// Initialize params
	if err := k.Params.Set(ctx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}

	return &fixture{
		ctx:          ctx,
		keeper:       k,
		addressCodec: addressCodec,
	}
}

// --- Setup Helper (Used by msg_server tests) ---
func setupCommonsKeeper(t *testing.T) (keeper.Keeper, sdk.Context, *mockBankKeeperCommons) {
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	memStoreKey := storetypes.NewMemoryStoreKey("mem_commons")
	authKey := storetypes.NewKVStoreKey(authtypes.StoreKey)

	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(memStoreKey, storetypes.StoreTypeMemory, nil)
	stateStore.MountStoreWithDB(authKey, storetypes.StoreTypeIAVL, db)

	require.NoError(t, stateStore.LoadLatestVersion())

	ctx := sdk.NewContext(stateStore, cmtproto.Header{Time: time.Now()}, false, log.NewNopLogger())

	// Explicitly register interfaces so the codec knows about BaseAccount
	cdcOpts := codectestutil.CodecOptions{}
	interfaceRegistry := cdcOpts.NewInterfaceRegistry()

	authtypes.RegisterInterfaces(interfaceRegistry)   // <--- Registers BaseAccount
	cryptocodec.RegisterInterfaces(interfaceRegistry) // <--- Registers PubKeys
	types.RegisterInterfaces(interfaceRegistry)       // <--- Registers Commons Types

	cdc := codec.NewProtoCodec(interfaceRegistry)
	addressCodec := addresscodec.NewBech32Codec("cosmos")
	authority := sdk.AccAddress([]byte("authority"))

	mockBK := &mockBankKeeperCommons{
		balance: make(map[string]sdk.Coins),
	}

	// Initialize Real Auth Keeper
	authKeeper := authkeeper.NewAccountKeeper(
		cdc,
		runtime.NewKVStoreService(authKey),
		authtypes.ProtoBaseAccount,
		map[string][]string{
			types.ModuleName:    nil,
			govtypes.ModuleName: {authtypes.Burner},
			"shield":            nil,
		},
		addresscodec.NewBech32Codec("cosmos"),
		"cosmos",
		authtypes.NewModuleAddress("gov").String(),
	)

	storeService := runtime.NewKVStoreService(storeKey)

	k := keeper.NewKeeper(
		storeService,
		cdc,
		addressCodec,
		authority,
		authKeeper,
		mockBK,
		mockFutarchyKeeper{},
		nil,
		mockSplitKeeper{},
		mockUpgradeKeeper{},
	)

	return k, ctx, mockBK
}

// --- Helper for tests that need full state setup ---
func setupSafeUpdateTest(t *testing.T) (keeper.Keeper, sdk.Context, sdk.AccAddress) {
	// 1. Define Keys
	key := storetypes.NewKVStoreKey(types.StoreKey)
	memKey := storetypes.NewMemoryStoreKey("mem_commons")
	tKey := storetypes.NewTransientStoreKey("transient_test")
	authKey := storetypes.NewKVStoreKey(authtypes.StoreKey)

	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())

	// 2. Mount Stores
	stateStore.MountStoreWithDB(key, storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(memKey, storetypes.StoreTypeMemory, nil)
	stateStore.MountStoreWithDB(tKey, storetypes.StoreTypeTransient, nil)
	stateStore.MountStoreWithDB(authKey, storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(storetypes.NewKVStoreKey(govtypes.StoreKey), storetypes.StoreTypeIAVL, db)

	require.NoError(t, stateStore.LoadLatestVersion())

	ctx := sdk.NewContext(stateStore, cmtproto.Header{Time: time.Now()}, false, log.NewNopLogger())

	// 3. Register Codec Interfaces
	cdcOpts := codectestutil.CodecOptions{}
	reg := cdcOpts.NewInterfaceRegistry()
	cryptocodec.RegisterInterfaces(reg)
	authtypes.RegisterInterfaces(reg)
	cdc := codec.NewProtoCodec(reg)

	// 4. Setup Real Auth Keeper
	maccPerms := map[string][]string{
		types.ModuleName:    nil,
		govtypes.ModuleName: {authtypes.Burner},
	}

	authK := authkeeper.NewAccountKeeper(
		cdc,
		runtime.NewKVStoreService(authKey),
		authtypes.ProtoBaseAccount,
		maccPerms,
		addresscodec.NewBech32Codec("cosmos"),
		"cosmos",
		authtypes.NewModuleAddress("gov").String(),
	)

	// 5. Setup Commons Keeper (no groupKeeper)
	k := keeper.NewKeeper(
		runtime.NewKVStoreService(key),
		cdc,
		authK.AddressCodec(),
		authK.GetModuleAddress("gov"),
		authK,
		nil,
		mockFutarchyKeeper{},
		nil,
		mockSplitKeeper{},
		mockUpgradeKeeper{},
	)

	return k, ctx, k.GetModuleAddress()
}

// --- Mock Auth Keeper ---
type mockAuthKeeper struct{}

func (m mockAuthKeeper) GetModuleAddress(name string) sdk.AccAddress {
	return authtypes.NewModuleAddress(name)
}

func (m mockAuthKeeper) AddressCodec() address.Codec {
	return addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
}

func (m mockAuthKeeper) IterateAccounts(ctx context.Context, cb func(account sdk.AccountI) bool) {
	// No-op for msg server tests
}

func (m mockAuthKeeper) GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI {
	// Return a dummy base account so we don't crash if something checks for existence
	if addr.Empty() {
		return nil
	}
	return authtypes.NewBaseAccountWithAddress(addr)
}

func (m mockAuthKeeper) NewAccount(ctx context.Context, acc sdk.AccountI) sdk.AccountI {
	return acc
}

func (m mockAuthKeeper) RemoveAccount(ctx context.Context, acc sdk.AccountI) {
	// No-op
}

func (m mockAuthKeeper) SetAccount(ctx context.Context, acc sdk.AccountI) {
	// No-op
}

// --- Mock Bank Keeper ---
type mockBankKeeperCommons struct {
	balance map[string]sdk.Coins
}

func (m *mockBankKeeperCommons) GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	return m.balance[addr.String()]
}

func (m *mockBankKeeperCommons) MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	return nil
}

func (m *mockBankKeeperCommons) SendCoins(ctx context.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error {
	if m.balance[fromAddr.String()].IsAllGTE(amt) {
		m.balance[fromAddr.String()] = m.balance[fromAddr.String()].Sub(amt...)
		m.balance[toAddr.String()] = m.balance[toAddr.String()].Add(amt...)
		return nil
	}
	return sdkerrors.ErrInsufficientFunds
}

func (m *mockBankKeeperCommons) SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	return nil
}

func (m *mockBankKeeperCommons) SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	return nil
}

func (m *mockBankKeeperCommons) SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	return m.balance[addr.String()]
}

// --- Mock Distribution Keeper ---
type mockDistrKeeper struct{}

func (m mockDistrKeeper) FundCommunityPool(ctx context.Context, amount sdk.Coins, sender sdk.AccAddress) error {
	return nil
}

// --- Mock Staking Keeper ---
type mockStakingKeeper struct{}

func (m mockStakingKeeper) TotalBondedTokens(ctx context.Context) (math.Int, error) {
	return math.NewInt(1000000), nil
}
func (m mockStakingKeeper) IterateBondedValidatorsByPower(ctx context.Context, fn func(index int64, validator stakingtypes.ValidatorI) (stop bool)) error {
	return nil
}
func (m mockStakingKeeper) ValidatorAddressCodec() address.Codec {
	return addresscodec.NewBech32Codec("cosmosvaloper")
}
func (m mockStakingKeeper) IterateDelegations(ctx context.Context, delegator sdk.AccAddress, fn func(index int64, delegation stakingtypes.DelegationI) (stop bool)) error {
	return nil
}

// --- Mock Futarchy Keeper ---
type mockFutarchyKeeper struct{}

func (m mockFutarchyKeeper) CreateMarketInternal(ctx sdk.Context, creator sdk.AccAddress, symbol string, question string, durationBlocks int64, redemptionBlocks int64, liquidity sdk.Coin) (uint64, error) {
	return 0, nil
}

// --- Mock Split Keeper ---
type mockSplitKeeper struct{}

func (m mockSplitKeeper) SetShareByAddress(ctx context.Context, address string, weight uint64) {
	// No-op for testing
}

// --- Mock Upgrade Keeper ---
type mockUpgradeKeeper struct{}

func (m mockUpgradeKeeper) ScheduleUpgrade(ctx context.Context, plan upgradetypes.Plan) error {
	// We just log or return nil for unit tests.
	// A real keeper would store the plan.
	if plan.Name == "fail" {
		return sdkerrors.ErrInvalidRequest
	}
	return nil
}
