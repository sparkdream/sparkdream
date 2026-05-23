package guardian

import (
	"encoding/json"

	"cosmossdk.io/core/appmodule"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/grpc"

	"sparkdream/x/guardian/keeper"
	"sparkdream/x/guardian/types"
)

var (
	_ module.AppModuleBasic = (*AppModule)(nil)
	_ module.AppModule      = (*AppModule)(nil)
	_ module.HasGenesis     = (*AppModule)(nil)

	_ appmodule.AppModule = (*AppModule)(nil)
)

// AppModule wires the guardian keeper into the runtime app. Genesis is
// trivial (no state). Msg surface is a single MsgExec dispatched
// internally to a small allowlist of inner msg types.
//
// Keeper held by pointer so post-construction SetIdentityKeeper /
// SetMintKeeper / SetStakingKeeper calls are visible to the msg server.
type AppModule struct {
	cdc    codec.Codec
	keeper *keeper.Keeper
}

// NewAppModule constructs the guardian AppModule.
func NewAppModule(cdc codec.Codec, k *keeper.Keeper) AppModule {
	return AppModule{cdc: cdc, keeper: k}
}

// IsAppModule implements the appmodule.AppModule interface.
func (AppModule) IsAppModule() {}

// Name returns the module name.
func (AppModule) Name() string { return types.ModuleName }

// RegisterLegacyAminoCodec registers the amino codec.
func (AppModule) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes.
func (AppModule) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {
	if err := types.RegisterQueryHandlerClient(clientCtx.CmdContext, mux, types.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

// RegisterInterfaces registers msg/query interfaces.
func (AppModule) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// RegisterServices registers the Msg and Query services.
func (am AppModule) RegisterServices(registrar grpc.ServiceRegistrar) error {
	types.RegisterMsgServer(registrar, keeper.NewMsgServerImpl(am.keeper))
	types.RegisterQueryServer(registrar, keeper.NewQueryServerImpl(am.keeper))
	return nil
}

// DefaultGenesis returns an empty genesis (no state).
func (am AppModule) DefaultGenesis(_ codec.JSONCodec) json.RawMessage {
	return json.RawMessage("{}")
}

// ValidateGenesis is a no-op (no state).
func (am AppModule) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, _ json.RawMessage) error {
	return nil
}

// InitGenesis is a no-op (no state).
func (am AppModule) InitGenesis(_ sdk.Context, _ codec.JSONCodec, _ json.RawMessage) {}

// ExportGenesis returns an empty genesis (no state).
func (am AppModule) ExportGenesis(_ sdk.Context, _ codec.JSONCodec) json.RawMessage {
	return json.RawMessage("{}")
}

// ConsensusVersion: bump on state-breaking changes (none yet).
func (AppModule) ConsensusVersion() uint64 { return 1 }
