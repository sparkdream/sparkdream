package blog

import (
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"cosmossdk.io/depinject/appconfig"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
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

	// Optional cross-module keepers. Provided via depinject where possible to avoid
	// the value-copy issue: the AppModule captures a snapshot of the keeper at
	// ProvideModule time, so Set*Keeper calls in app.go (post-depinject) never reach
	// the msgServer embedded copy. Providing them here ensures the AppModule snapshot
	// already has them set.
	//
	// RepKeeper: blog → rep (no cycle; rep → season → blog is post-depinject only)
	// VoteKeeper: blog → vote (no cycle; vote → rep is post-depinject)
	// SeasonKeeper: NOT wired via depinject — blog uses DefaultEpochDuration for
	// nullifier scoping, not the voting epoch. Wired in app.go only.
	RepKeeper  types.RepKeeper  `optional:"true"`
	VoteKeeper types.VoteKeeper `optional:"true"`
}

type ModuleOutputs struct {
	depinject.Out

	BlogKeeper keeper.Keeper
	Module     appmodule.AppModule
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
		nil, // CommonsKeeper wired in app.go
		in.RepKeeper,
	)
	// Wire optional keepers that are available at depinject time.
	// This ensures the AppModule's keeper snapshot already has these set,
	// avoiding the value-copy issue where app.go Set*Keeper calls are invisible
	// to the msgServer (which embeds the AppModule's copy of the keeper).
	if in.VoteKeeper != nil {
		k.SetVoteKeeper(in.VoteKeeper)
	}
	// SeasonKeeper wired in app.go only (blog uses DefaultEpochDuration, not the voting epoch)
	// CommonsKeeper wired in app.go (no cycle, but wired there for consistency)
	m := NewAppModule(in.Cdc, k, in.AuthKeeper, in.BankKeeper)

	return ModuleOutputs{BlogKeeper: k, Module: m}
}
