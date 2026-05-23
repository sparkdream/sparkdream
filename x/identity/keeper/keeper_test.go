package keeper_test

import (
	"context"
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"

	"sparkdream/x/identity/keeper"
	module "sparkdream/x/identity/module"
	"sparkdream/x/identity/types"
)

// fixture is the bare keeper used by unit tests. A mock bank is wired by
// default (H1 requires non-nil bank for non-empty-identity InitGenesis); the
// mock is exposed via fixture.bank so tests can introspect what got written.
type fixture struct {
	ctx    context.Context
	keeper *keeper.Keeper
	bank   *mockBank
}

// newValidIdentity returns a canonical valid ChainIdentity for tests.
func newValidIdentity() types.ChainIdentity {
	return types.ChainIdentity{
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

func initFixture(t *testing.T) *fixture {
	t.Helper()
	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx
	// Use a mock bank by default so tests calling InitGenesis with a non-empty
	// identity satisfy the H1 requirement (bankKeeper must be non-nil).
	// fixture.bank is exposed so tests can assert on what got written.
	bank := newMockBank()
	k := keeper.NewKeeper(storeService, encCfg.Codec, bank)
	return &fixture{ctx: ctx, keeper: &k, bank: bank}
}
