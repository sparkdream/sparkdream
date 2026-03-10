package commons

import (
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"cosmossdk.io/depinject/appconfig"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
	futarchykeeper "sparkdream/x/futarchy/keeper"
	splitkeeper "sparkdream/x/split/keeper"
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

	AuthKeeper     types.AuthKeeper
	BankKeeper     types.BankKeeper
	FutarchyKeeper futarchykeeper.Keeper
	SplitKeeper    splitkeeper.Keeper
	UpgradeKeeper  types.UpgradeKeeper
}

type ModuleOutputs struct {
	depinject.Out

	CommonsKeeper keeper.Keeper
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
		in.AuthKeeper,
		in.BankKeeper,
		in.FutarchyKeeper,
		nil, // GovKeeper wired manually in app.go via SetGovKeeper()
		in.SplitKeeper,
		in.UpgradeKeeper,
	)
	m := NewAppModule(in.Cdc, k, in.AuthKeeper, in.BankKeeper)

	return ModuleOutputs{CommonsKeeper: k, Module: m}
}
