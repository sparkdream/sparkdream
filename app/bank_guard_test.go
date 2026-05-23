package app

import (
	"context"
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	identityk "sparkdream/x/identity/keeper"
	identitymodule "sparkdream/x/identity/module"
	identitytypes "sparkdream/x/identity/types"
)

// fakeBank implements just enough of bankkeeper.Keeper to test the guard. It
// records the last SetDenomMetaData call. The bank keeper.Keeper interface
// has many methods that the wrapper embeds and forwards unchanged; the guard
// override is what we want to exercise.
//
// We satisfy the interface by embedding it as a struct-typed nil; methods
// other than SetDenomMetaData panic if invoked, which the tests don't do.
type fakeBank struct {
	bankkeeper.Keeper
	captured []banktypes.Metadata
}

func (f *fakeBank) SetDenomMetaData(_ context.Context, md banktypes.Metadata) {
	f.captured = append(f.captured, md)
}

// validIdentity is the canonical Phoenix identity used in app-layer tests.
func validIdentity() identitytypes.ChainIdentity {
	return identitytypes.ChainIdentity{
		ChainHumanName:       "Phoenix",
		ChainTickerPrefix:    "PHX",
		BondDenom:            "upspk.phoenix",
		BondDisplaySymbol:    "PSPK",
		BondDisplayName:      "Phoenix Spark",
		BondDisplayDecimals:  6,
		DreamDenom:           "udream.phoenix",
		DreamDisplaySymbol:   "PDRM",
		DreamDisplayName:     "Phoenix Dream",
		DreamDisplayDecimals: 6,
		FoundedAt:            1735689600,
	}
}

func setupKeeperWithSeal(t *testing.T) (context.Context, *identityk.Keeper) {
	t.Helper()
	encCfg := moduletestutil.MakeTestEncodingConfig(identitymodule.AppModule{})
	storeKey := storetypes.NewKVStoreKey(identitytypes.StoreKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx
	// H1 requires a non-nil bank keeper for non-empty-identity InitGenesis.
	// Use a fakeBank as the seed bank; the wrapper test then constructs a
	// separate fakeBank for the guard layer being exercised.
	k := identityk.NewKeeper(runtime.NewKVStoreService(storeKey), encCfg.Codec, &fakeBank{})
	require.NoError(t, k.InitGenesis(ctx, identitytypes.GenesisState{Identity: validIdentity(), AllowChainIdMismatch: true}))
	return ctx, &k
}

func TestWrapperPassesThroughPreSeal(t *testing.T) {
	// Identity keeper without InitGenesis -> guard treats as pre-seal and
	// passes any metadata through.
	encCfg := moduletestutil.MakeTestEncodingConfig(identitymodule.AppModule{})
	storeKey := storetypes.NewKVStoreKey(identitytypes.StoreKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx
	k := identityk.NewKeeper(runtime.NewKVStoreService(storeKey), encCfg.Codec, nil)
	fake := &fakeBank{}
	w := WrapBankKeeperWithIdentityGuard(fake, &k)
	w.SetDenomMetaData(ctx, banktypes.Metadata{Base: "upspk.phoenix", Symbol: "ANYTHING"})
	require.Len(t, fake.captured, 1)
}

func TestWrapperAllowsForeignDenom(t *testing.T) {
	ctx, k := setupKeeperWithSeal(t)
	fake := &fakeBank{}
	w := WrapBankKeeperWithIdentityGuard(fake, k)
	w.SetDenomMetaData(ctx, banktypes.Metadata{
		Base: "ibc/ABCDEF", Symbol: "FOREIGN", Display: "foreign",
	})
	require.Len(t, fake.captured, 1)
}

func TestWrapperAllowsDescriptionEditOnNativeDenom(t *testing.T) {
	ctx, k := setupKeeperWithSeal(t)
	fake := &fakeBank{}
	w := WrapBankKeeperWithIdentityGuard(fake, k)
	// Canonical Symbol+Display retained, description tweak allowed.
	w.SetDenomMetaData(ctx, banktypes.Metadata{
		Base:        "upspk.phoenix",
		Symbol:      "PSPK",
		Display:     "pspk",
		Description: "Phoenix Spark, updated description text",
	})
	require.Len(t, fake.captured, 1)
}

func TestWrapperRejectsNativeSymbolTamper(t *testing.T) {
	ctx, k := setupKeeperWithSeal(t)
	fake := &fakeBank{}
	w := WrapBankKeeperWithIdentityGuard(fake, k)
	require.Panics(t, func() {
		w.SetDenomMetaData(ctx, banktypes.Metadata{
			Base: "upspk.phoenix", Symbol: "TAMPERED", Display: "pspk",
		})
	})
	require.Empty(t, fake.captured)
}

func TestWrapperRejectsNativeDisplayTamper(t *testing.T) {
	ctx, k := setupKeeperWithSeal(t)
	fake := &fakeBank{}
	w := WrapBankKeeperWithIdentityGuard(fake, k)
	require.Panics(t, func() {
		w.SetDenomMetaData(ctx, banktypes.Metadata{
			Base: "upspk.phoenix", Symbol: "PSPK", Display: "tampered",
		})
	})
}

func TestWrapperRejectsDreamSymbolTamper(t *testing.T) {
	ctx, k := setupKeeperWithSeal(t)
	fake := &fakeBank{}
	w := WrapBankKeeperWithIdentityGuard(fake, k)
	require.Panics(t, func() {
		w.SetDenomMetaData(ctx, banktypes.Metadata{
			Base: "udream.phoenix", Symbol: "TAMPER", Display: "pdrm",
		})
	})
}
