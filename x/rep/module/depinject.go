package rep

import (
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"cosmossdk.io/depinject/appconfig"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
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

	AuthKeeper types.AuthKeeper
	BankKeeper types.BankKeeper

	// Optional cross-module keepers (nil if modules not available)
	CommonsKeeper types.CommonsKeeper `optional:"true"`
	SeasonKeeper  types.SeasonKeeper  `optional:"true"`
	// VoteKeeper is wired manually in app.go via SetVoteKeeper to break
	// the cyclic dependency: vote → rep → vote.
}

type ModuleOutputs struct {
	depinject.Out

	RepKeeper keeper.Keeper
	Module    appmodule.AppModule
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
		authority.Bytes(),
		in.AuthKeeper,
		in.BankKeeper,
		in.CommonsKeeper,
		in.SeasonKeeper,
		nil, // VoteKeeper wired via SetVoteKeeper in app.go
	)
	m := NewAppModule(in.Cdc, k, in.AuthKeeper, in.BankKeeper)

	return ModuleOutputs{RepKeeper: k, Module: m}
}
