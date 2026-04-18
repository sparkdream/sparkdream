package keeper_test

import (
	"context"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Helper for pointer to Int
func PtrInt(i math.Int) *math.Int {
	return &i
}

// Helper for pointer to uint32
func PtrUint32(u uint32) *uint32 {
	return &u
}

// Helper for pointer to Dec
func PtrDec(d math.LegacyDec) *math.LegacyDec {
	return &d
}

// mockAuthKeeper mocks the auth keeper
type mockAuthKeeper struct {
	GetModuleAddressFn func(name string) sdk.AccAddress
}

func (m mockAuthKeeper) GetModuleAddress(name string) sdk.AccAddress {
	if m.GetModuleAddressFn != nil {
		return m.GetModuleAddressFn(name)
	}
	// Return a dummy address
	return sdk.AccAddress([]byte("module_address_" + name))
}

func (m mockAuthKeeper) GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI {
	// Not needed for these tests yet
	return nil
}

// mockBankKeeper mocks the bank keeper
type mockBankKeeper struct {
	SpendableCoinsFn               func(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	GetBalanceFn                   func(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	SendCoinsFromAccountToModuleFn func(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccountFn func(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	MintCoinsFn                    func(ctx context.Context, moduleName string, amt sdk.Coins) error
	BurnCoinsFn                    func(ctx context.Context, moduleName string, amt sdk.Coins) error
}

func (m mockBankKeeper) SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	if m.SpendableCoinsFn != nil {
		return m.SpendableCoinsFn(ctx, addr)
	}
	return sdk.Coins{}
}

func (m mockBankKeeper) GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	if m.GetBalanceFn != nil {
		return m.GetBalanceFn(ctx, addr, denom)
	}
	return sdk.NewCoin(denom, math.ZeroInt())
}

func (m mockBankKeeper) SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	if m.SendCoinsFromAccountToModuleFn != nil {
		return m.SendCoinsFromAccountToModuleFn(ctx, senderAddr, recipientModule, amt)
	}
	return nil
}

func (m mockBankKeeper) SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	if m.SendCoinsFromModuleToAccountFn != nil {
		return m.SendCoinsFromModuleToAccountFn(ctx, senderModule, recipientAddr, amt)
	}
	return nil
}

func (m mockBankKeeper) MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	if m.MintCoinsFn != nil {
		return m.MintCoinsFn(ctx, moduleName, amt)
	}
	return nil
}

func (m mockBankKeeper) BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	if m.BurnCoinsFn != nil {
		return m.BurnCoinsFn(ctx, moduleName, amt)
	}
	return nil
}

// mockCommonsKeeper mocks the commons keeper (already defined in keeper_test.go, but useful to have here for shared usage if needed)
// We will reuse the one in keeper_test.go if accessible, otherwise redefine here or move to common file.
// Since they are in the same package (keeper_test), the one in keeper_test.go is available.
