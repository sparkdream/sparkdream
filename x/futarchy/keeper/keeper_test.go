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
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/futarchy/keeper"
	module "sparkdream/x/futarchy/module"
	"sparkdream/x/futarchy/types"
)

// ----------------------------------------------------------------------------
// MockBankKeeper (Stateful)
// ----------------------------------------------------------------------------
// We define a mock that satisfies the types.BankKeeper interface and holds state.
type MockBankKeeper struct {
	BalanceMap map[string]sdk.Coin // Key: Address+Denom
}

func NewMockBankKeeper() *MockBankKeeper {
	return &MockBankKeeper{
		BalanceMap: make(map[string]sdk.Coin),
	}
}

// Helper to set balance for tests
func (m *MockBankKeeper) SetBalance(addr sdk.AccAddress, coin sdk.Coin) {
	key := addr.String() + coin.Denom
	m.BalanceMap[key] = coin
}

// Interface implementation: GetBalance
func (m *MockBankKeeper) GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	key := addr.String() + denom
	if coin, ok := m.BalanceMap[key]; ok {
		return coin
	}
	return sdk.NewCoin(denom, math.NewInt(0))
}

func (m *MockBankKeeper) SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	return sdk.NewCoins()
}

func (m *MockBankKeeper) SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	for _, coin := range amt {
		key := senderAddr.String() + coin.Denom
		balance, ok := m.BalanceMap[key]
		if !ok || balance.Amount.LT(coin.Amount) {
			return sdkerrors.ErrInsufficientFunds
		}
		newAmount := balance.Amount.Sub(coin.Amount)
		m.BalanceMap[key] = sdk.NewCoin(coin.Denom, newAmount)
	}
	return nil
}

func (m *MockBankKeeper) MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	return nil
}

func (m *MockBankKeeper) SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	for _, coin := range amt {
		key := recipientAddr.String() + coin.Denom
		balance, ok := m.BalanceMap[key]
		if !ok {
			balance = sdk.NewCoin(coin.Denom, math.ZeroInt())
		}
		newAmount := balance.Amount.Add(coin.Amount)
		m.BalanceMap[key] = sdk.NewCoin(coin.Denom, newAmount)
	}
	return nil
}

func (m *MockBankKeeper) BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	return nil
}

func (m *MockBankKeeper) SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error {
	// For testing purposes, we can just ignore this or track it if needed
	return nil
}

// ----------------------------------------------------------------------------
// MockAuthKeeper
// ----------------------------------------------------------------------------

type MockAuthKeeper struct{}

func (m *MockAuthKeeper) AddressCodec() address.Codec {
	return addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
}

func (m *MockAuthKeeper) GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI {
	return nil
}

func (m *MockAuthKeeper) GetModuleAddress(moduleName string) sdk.AccAddress {
	return nil // Return nil to skip fee collection in tests
}

// ----------------------------------------------------------------------------
// Test Fixture
// ----------------------------------------------------------------------------

type fixture struct {
	ctx          context.Context
	keeper       keeper.Keeper
	addressCodec address.Codec
	bankKeeper   *MockBankKeeper // Exposed so tests can SetBalance
}

func initFixture(t *testing.T) *fixture {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	authority := authtypes.NewModuleAddress(types.GovModuleName)

	// Create and inject the stateful mocks
	mockBank := NewMockBankKeeper()
	mockAuth := &MockAuthKeeper{}

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
		mockAuth,
		mockBank,
	)

	// Initialize params
	if err := k.Params.Set(ctx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}

	return &fixture{
		ctx:          ctx,
		keeper:       k,
		addressCodec: addressCodec,
		bankKeeper:   mockBank,
	}
}
