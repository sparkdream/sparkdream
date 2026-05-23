package federation

import (
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"cosmossdk.io/depinject/appconfig"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	ibckeeper "github.com/cosmos/ibc-go/v10/modules/core/keeper"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
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

	IBCKeeperFn func() *ibckeeper.Keeper `optional:"true"`

	// IdentityKeeper supplies the chain's bond/dream denoms at runtime. Optional
	// so depinject can wire in the order it likes; the keeper is set on the
	// federation keeper before NewAppModule snapshots the value, otherwise the
	// msg_server's embedded copy would never see it (see
	// docs/development-conventions.md "AppModule Value-Copy Bug").
	// Federation uses `late` (shared pointer) for its other
	// post-depinject keepers so the pattern is identical here.
	IdentityKeeper types.IdentityKeeper `optional:"true"`
}

type ModuleOutputs struct {
	depinject.Out

	FederationKeeper keeper.Keeper
	Module           appmodule.AppModule
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
		in.IBCKeeperFn,
	)
	if in.IdentityKeeper != nil {
		k.SetIdentityKeeper(in.IdentityKeeper)
	}
	m := NewAppModule(in.Cdc, k, in.AuthKeeper, in.BankKeeper)

	return ModuleOutputs{FederationKeeper: k, Module: m}
}
