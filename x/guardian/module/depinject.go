package guardian

import (
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"cosmossdk.io/depinject/appconfig"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"sparkdream/x/guardian/keeper"
	"sparkdream/x/guardian/types"
)

var _ depinject.OnePerModuleType = AppModule{}

// IsOnePerModuleType implements depinject.OnePerModuleType.
func (AppModule) IsOnePerModuleType() {}

func init() {
	appconfig.Register(
		&types.Module{},
		appconfig.Provide(ProvideModule),
	)
}

// ModuleInputs declares depinject inputs. Guardian takes the msg service
// router (for inner-msg dispatch) and the address codec; downstream
// keepers are wired post-depinject in app.go.
type ModuleInputs struct {
	depinject.In

	Config       *types.Module
	StoreService store.KVStoreService
	Cdc          codec.Codec
	AddressCodec address.Codec
	MsgRouter    baseapp.MessageRouter
}

// ModuleOutputs emits the guardian keeper (as pointer, so downstream
// SetIdentityKeeper / SetMintKeeper / SetStakingKeeper updates are
// visible) and the AppModule.
type ModuleOutputs struct {
	depinject.Out

	GuardianKeeper *keeper.Keeper
	Module         appmodule.AppModule
}

// ProvideModule constructs the keeper. The authority is the gov module
// account address (gov is the only entity allowed to invoke MsgExec in
// production wiring).
func ProvideModule(in ModuleInputs) ModuleOutputs {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	k := keeper.NewKeeper(in.StoreService, in.Cdc, in.AddressCodec, in.MsgRouter, authority)
	m := NewAppModule(in.Cdc, &k)
	return ModuleOutputs{GuardianKeeper: &k, Module: m}
}
