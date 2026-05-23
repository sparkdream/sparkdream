package collect

import (
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"cosmossdk.io/depinject/appconfig"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
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

type ModuleInputs struct {
	depinject.In

	Config       *types.Module
	StoreService store.KVStoreService
	Cdc          codec.Codec
	AddressCodec address.Codec

	AuthKeeper    types.AuthKeeper
	BankKeeper    types.BankKeeper
	RepKeeper     types.RepKeeper     `optional:"true"`
	CommonsKeeper types.CommonsKeeper `optional:"true"`
	BlogKeeper    types.BlogKeeper    `optional:"true"`
	ForumKeeper   types.ForumKeeper   `optional:"true"`

	// IdentityKeeper supplies the chain's bond/dream denoms at runtime.
	// Wired here (before NewAppModule) rather than via app.go because the
	// AppModule captures the keeper by value; the msg_server's embedded copy
	// would otherwise never see a post-depinject SetIdentityKeeper. See
	// docs/development-conventions.md "AppModule Value-Copy Bug".
	IdentityKeeper types.IdentityKeeper `optional:"true"`
}

type ModuleOutputs struct {
	depinject.Out

	CollectKeeper keeper.Keeper
	Module        appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	// default to governance authority if not provided
	authority := authtypes.NewModuleAddress(types.GovModuleName)
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority)
	}
	k := keeper.NewKeeper(
		in.StoreService,
		in.Cdc,
		in.AddressCodec,
		authority,
		in.BankKeeper,
		in.CommonsKeeper,
		in.ForumKeeper,
	)
	// RepKeeper is optional at depinject time to break cyclic dependency
	// (rep → season → collect → rep). Wired manually in app.go via SetRepKeeper.
	if in.RepKeeper != nil {
		k.SetRepKeeper(in.RepKeeper)
	}
	if in.BlogKeeper != nil {
		k.SetBlogKeeper(in.BlogKeeper)
	}
	if in.IdentityKeeper != nil {
		k.SetIdentityKeeper(in.IdentityKeeper)
	}
	m := NewAppModule(in.Cdc, k, in.AuthKeeper, in.BankKeeper)

	return ModuleOutputs{CollectKeeper: k, Module: m}
}
