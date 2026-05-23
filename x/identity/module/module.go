package identity

import (
	"encoding/json"
	"fmt"

	"cosmossdk.io/core/appmodule"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/grpc"

	"sparkdream/x/identity/keeper"
	"sparkdream/x/identity/types"
)

var (
	_ module.AppModuleBasic = (*AppModule)(nil)
	_ module.AppModule      = (*AppModule)(nil)
	_ module.HasGenesis     = (*AppModule)(nil)
	_ module.HasInvariants  = (*AppModule)(nil)

	_ appmodule.AppModule = (*AppModule)(nil)
)

// AppModule wires the identity keeper into the runtime app. No msg surface;
// queries only.
//
// IMPORTANT: keeper is held by POINTER, not value. Late-bound dependencies
// (bank, staking, mint keepers, see app.go) mutate the keeper struct after
// AppModule construction. A value-copy would freeze those keeper references
// at their construction-time value (nil), causing invariants 3-5 to
// silently no-op forever. See docs/development-conventions.md "AppModule
// Value-Copy Bug" for the canonical project example.
type AppModule struct {
	cdc    codec.Codec
	keeper *keeper.Keeper
}

// NewAppModule constructs the identity AppModule. Staking and mint keepers
// are wired post-construction via Keeper.SetStakingKeeper /
// Keeper.SetMintKeeper from app.go.
func NewAppModule(cdc codec.Codec, k *keeper.Keeper) AppModule {
	return AppModule{cdc: cdc, keeper: k}
}

// IsAppModule implements the appmodule.AppModule interface.
func (AppModule) IsAppModule() {}

// Name returns the name of the module as a string.
func (AppModule) Name() string { return types.ModuleName }

// RegisterLegacyAminoCodec is a no-op: identity has no Msg types.
func (AppModule) RegisterLegacyAminoCodec(*codec.LegacyAmino) {}

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the module.
func (AppModule) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {
	if err := types.RegisterQueryHandlerClient(clientCtx.CmdContext, mux, types.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

// RegisterInterfaces is a no-op: identity has no Msg types.
func (AppModule) RegisterInterfaces(registrar codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registrar)
}

// RegisterServices registers the Query service. No Msg service to register.
func (am AppModule) RegisterServices(registrar grpc.ServiceRegistrar) error {
	types.RegisterQueryServer(registrar, keeper.NewQueryServerImpl(am.keeper))
	return nil
}

// RegisterInvariants registers the §16 invariants. The keeper pointer is
// passed through so the invariant closures read live keeper fields (notably
// bankKeeper, stakingKeeper, mintKeeper, which are late-bound from app.go)
// on every block, not a stale snapshot.
func (am AppModule) RegisterInvariants(ir sdk.InvariantRegistry) {
	keeper.RegisterInvariants(ir, am.keeper)
}

// DefaultGenesis returns a placeholder GenesisState. Real chains MUST
// overwrite it (DefaultGenesis is intentionally a non-functional shape).
func (am AppModule) DefaultGenesis(codec.JSONCodec) json.RawMessage {
	return am.cdc.MustMarshalJSON(types.DefaultGenesis())
}

// ValidateGenesis runs intrinsic validation.
func (am AppModule) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var genState types.GenesisState
	if err := am.cdc.UnmarshalJSON(bz, &genState); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}
	return genState.Validate()
}

// InitGenesis runs the module's genesis sequence.
func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, gs json.RawMessage) {
	var genState types.GenesisState
	if err := am.cdc.UnmarshalJSON(gs, &genState); err != nil {
		panic(fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err))
	}
	if err := am.keeper.InitGenesis(ctx, genState); err != nil {
		panic(fmt.Errorf("failed to initialize %s genesis state: %w", types.ModuleName, err))
	}
}

// ExportGenesis returns the module's exported genesis state.
func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	genState, err := am.keeper.ExportGenesis(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to export %s genesis state: %w", types.ModuleName, err))
	}
	bz, err := am.cdc.MarshalJSON(genState)
	if err != nil {
		panic(fmt.Errorf("failed to marshal %s genesis state: %w", types.ModuleName, err))
	}
	return bz
}

// ConsensusVersion is a sequence number for state-breaking change of the
// module.
func (AppModule) ConsensusVersion() uint64 { return 1 }
