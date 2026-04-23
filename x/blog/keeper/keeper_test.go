package keeper_test

import (
	"context"
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

	"sparkdream/x/blog/keeper"
	module "sparkdream/x/blog/module"
	"sparkdream/x/blog/types"

	reptypes "sparkdream/x/rep/types"
)

// mockBankKeeper implements types.BankKeeper for testing
type mockBankKeeper struct {
	SpendableCoinsFn               func(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	SendCoinsFromAccountToModuleFn func(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	BurnCoinsFn                    func(ctx context.Context, moduleName string, amt sdk.Coins) error
	// Track calls for assertion
	SendCoinsFromAccountToModuleCalls []sendCoinsCall
	BurnCoinsCalls                    []burnCoinsCall
}

type sendCoinsCall struct {
	SenderAddr      sdk.AccAddress
	RecipientModule string
	Amt             sdk.Coins
}

type burnCoinsCall struct {
	ModuleName string
	Amt        sdk.Coins
}

func (m *mockBankKeeper) SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	if m.SpendableCoinsFn != nil {
		return m.SpendableCoinsFn(ctx, addr)
	}
	return sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1000000000)))
}

func (m *mockBankKeeper) SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	m.SendCoinsFromAccountToModuleCalls = append(m.SendCoinsFromAccountToModuleCalls, sendCoinsCall{
		SenderAddr:      senderAddr,
		RecipientModule: recipientModule,
		Amt:             amt,
	})
	if m.SendCoinsFromAccountToModuleFn != nil {
		return m.SendCoinsFromAccountToModuleFn(ctx, senderAddr, recipientModule, amt)
	}
	return nil
}

func (m *mockBankKeeper) BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	m.BurnCoinsCalls = append(m.BurnCoinsCalls, burnCoinsCall{
		ModuleName: moduleName,
		Amt:        amt,
	})
	if m.BurnCoinsFn != nil {
		return m.BurnCoinsFn(ctx, moduleName, amt)
	}
	return nil
}

// mockRepKeeper implements types.RepKeeper for testing
type mockRepKeeper struct {
	IsActiveMemberFn                 func(ctx context.Context, addr sdk.AccAddress) bool
	GetTrustLevelFn                  func(ctx context.Context, addr sdk.AccAddress) (reptypes.TrustLevel, error)
	GetMemberTrustTreeRootFn         func(ctx context.Context) ([]byte, error)
	GetPreviousMemberTrustTreeRootFn func(ctx context.Context) []byte
	CreateAuthorBondFn               func(ctx context.Context, author sdk.AccAddress, targetType reptypes.StakeTargetType, targetID uint64, amount math.Int) (uint64, error)
	SlashAuthorBondFn                func(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) error
	GetAuthorBondFn                  func(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) (reptypes.Stake, error)
	GetContentConvictionFn           func(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) (math.LegacyDec, error)
	GetContentStakesFn               func(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) ([]reptypes.Stake, error)

	// Tag registry behavior — tests set these to configure the fake registry.
	// KnownTags: tag names that exist in the registry.
	// ReservedTags: subset of KnownTags that are reserved (rejected).
	KnownTags    map[string]bool
	ReservedTags map[string]bool

	// Track calls
	CreateAuthorBondCalls   []createAuthorBondCall
	IncrementTagUsageCalls  []incrementTagUsageCall
}

type incrementTagUsageCall struct {
	Name      string
	Timestamp int64
}

type createAuthorBondCall struct {
	Author     sdk.AccAddress
	TargetType reptypes.StakeTargetType
	TargetID   uint64
	Amount     math.Int
}

func (m *mockRepKeeper) IsActiveMember(ctx context.Context, addr sdk.AccAddress) bool {
	if m.IsActiveMemberFn != nil {
		return m.IsActiveMemberFn(ctx, addr)
	}
	return true
}

func (m *mockRepKeeper) GetTrustLevel(ctx context.Context, addr sdk.AccAddress) (reptypes.TrustLevel, error) {
	if m.GetTrustLevelFn != nil {
		return m.GetTrustLevelFn(ctx, addr)
	}
	return reptypes.TrustLevel_TRUST_LEVEL_CORE, nil // default: CORE (highest) — tests pass unless overridden
}

func (m *mockRepKeeper) GetMemberTrustTreeRoot(ctx context.Context) ([]byte, error) {
	if m.GetMemberTrustTreeRootFn != nil {
		return m.GetMemberTrustTreeRootFn(ctx)
	}
	return []byte("mock-trust-tree-root"), nil
}

func (m *mockRepKeeper) GetPreviousMemberTrustTreeRoot(ctx context.Context) []byte {
	if m.GetPreviousMemberTrustTreeRootFn != nil {
		return m.GetPreviousMemberTrustTreeRootFn(ctx)
	}
	return nil
}

func (m *mockRepKeeper) GetContentConviction(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) (math.LegacyDec, error) {
	if m.GetContentConvictionFn != nil {
		return m.GetContentConvictionFn(ctx, targetType, targetID)
	}
	return math.LegacyZeroDec(), nil
}

func (m *mockRepKeeper) GetContentStakes(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) ([]reptypes.Stake, error) {
	if m.GetContentStakesFn != nil {
		return m.GetContentStakesFn(ctx, targetType, targetID)
	}
	return nil, nil
}

func (m *mockRepKeeper) CreateAuthorBond(ctx context.Context, author sdk.AccAddress, targetType reptypes.StakeTargetType, targetID uint64, amount math.Int) (uint64, error) {
	m.CreateAuthorBondCalls = append(m.CreateAuthorBondCalls, createAuthorBondCall{
		Author:     author,
		TargetType: targetType,
		TargetID:   targetID,
		Amount:     amount,
	})
	if m.CreateAuthorBondFn != nil {
		return m.CreateAuthorBondFn(ctx, author, targetType, targetID, amount)
	}
	return 1, nil
}

func (m *mockRepKeeper) SlashAuthorBond(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) error {
	if m.SlashAuthorBondFn != nil {
		return m.SlashAuthorBondFn(ctx, targetType, targetID)
	}
	return nil
}

func (m *mockRepKeeper) GetAuthorBond(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) (reptypes.Stake, error) {
	if m.GetAuthorBondFn != nil {
		return m.GetAuthorBondFn(ctx, targetType, targetID)
	}
	return reptypes.Stake{}, reptypes.ErrAuthorBondNotFound
}

func (m *mockRepKeeper) ValidateInitiativeReference(_ context.Context, _ uint64) error {
	return nil
}

func (m *mockRepKeeper) RegisterContentInitiativeLink(_ context.Context, _ uint64, _ int32, _ uint64) error {
	return nil
}

func (m *mockRepKeeper) RemoveContentInitiativeLink(_ context.Context, _ uint64, _ int32, _ uint64) error {
	return nil
}

func (m *mockRepKeeper) TagExists(_ context.Context, name string) (bool, error) {
	if m.KnownTags == nil {
		// Default to permissive — any tag is accepted — so existing tests
		// that don't set KnownTags but do pass tags aren't broken. Tag-specific
		// tests populate KnownTags explicitly.
		return true, nil
	}
	return m.KnownTags[name], nil
}

func (m *mockRepKeeper) IsReservedTag(_ context.Context, name string) (bool, error) {
	return m.ReservedTags[name], nil
}

func (m *mockRepKeeper) IncrementTagUsage(_ context.Context, name string, timestamp int64) error {
	m.IncrementTagUsageCalls = append(m.IncrementTagUsageCalls, incrementTagUsageCall{Name: name, Timestamp: timestamp})
	return nil
}

type fixture struct {
	ctx          context.Context
	keeper       keeper.Keeper
	addressCodec address.Codec
	bankKeeper   *mockBankKeeper
	repKeeper    *mockRepKeeper
}

func initFixture(t *testing.T) *fixture {
	t.Helper()

	// Ensure global bech32 prefix matches the address codec so that
	// sdk.AccAddressFromBech32 works for sprkdrm addresses (e.g. in EndBlock).
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("sprkdrm", "sprkdrmpub")

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec("sprkdrm")
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	authority := authtypes.NewModuleAddress(types.GovModuleName)

	bankKeeper := &mockBankKeeper{}
	repKeeper := &mockRepKeeper{}

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
		bankKeeper,
		nil, // commonsKeeper (optional)
		repKeeper,
	)

	// Initialize params
	if err := k.Params.Set(ctx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}

	return &fixture{
		ctx:          ctx,
		keeper:       k,
		addressCodec: addressCodec,
		bankKeeper:   bankKeeper,
		repKeeper:    repKeeper,
	}
}
