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

type fixture struct {
	ctx          context.Context
	keeper       keeper.Keeper
	addressCodec address.Codec
	bankKeeper   *mockBankKeeper
}

func initFixture(t *testing.T) *fixture {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	authority := authtypes.NewModuleAddress(types.GovModuleName)

	bankKeeper := &mockBankKeeper{}

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
		bankKeeper,
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
	}
}
