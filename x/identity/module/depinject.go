package identity

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"cosmossdk.io/depinject/appconfig"
	"github.com/cosmos/cosmos-sdk/codec"

	"sparkdream/x/identity/keeper"
	"sparkdream/x/identity/types"
)

var _ depinject.OnePerModuleType = AppModule{}

// IsOnePerModuleType implements the depinject.OnePerModuleType interface.
func (AppModule) IsOnePerModuleType() {}

func init() {
	appconfig.Register(
		&types.Module{},
		appconfig.Provide(ProvideModule),
	)
}

// ModuleInputs is intentionally narrow. x/identity is a leaf module: it
// declares no SDK keeper dependencies and exposes the identity keeper to
// every consumer via the per-module IdentityKeeper output. The bank keeper
// is wired post-depinject through SetBankKeeper (see app.go) so the bank
// guard wrapper can compose the two keepers without a depinject cycle.
type ModuleInputs struct {
	depinject.In

	Config       *types.Module
	StoreService store.KVStoreService
	Cdc          codec.Codec
}

type ModuleOutputs struct {
	depinject.Out

	// IdentityKeeper is emitted as *Keeper so that late-bound dependencies
	// (the bank keeper wired via SetBankKeeper from app.go) propagate to
	// every consumer through the same pointer. A value-emit would freeze the
	// keeper's bankKeeper field at nil; the AppModule's invariants would
	// silently no-op. See docs/development-conventions.md "AppModule Value-Copy Bug".
	IdentityKeeper *keeper.Keeper
	Module         appmodule.AppModule
}

// ProvideModule constructs the keeper without bank/staking/mint keepers.
// The app wires them post-depinject via Keeper.Set*Keeper(...) so that the
// bank guard (which depends on the identity keeper) can be composed without
// circular construction, and so that x/identity remains a true leaf in the
// depinject graph.
func ProvideModule(in ModuleInputs) ModuleOutputs {
	k := keeper.NewKeeper(in.StoreService, in.Cdc, nil)
	m := NewAppModule(in.Cdc, &k)
	return ModuleOutputs{IdentityKeeper: &k, Module: m}
}
