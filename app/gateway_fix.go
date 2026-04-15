package app

// gateway_fix.go fixes REST API panics from proto v2 reflection on gogoproto
// custom types (math.Int, math.LegacyDec, *time.Time).
//
// Root cause: The gRPC-gateway handlers use client.Context.Invoke, which
// delegates to the gRPC client connection. Somewhere in the gRPC/gateway
// pipeline, proto v2 reflection is triggered on gogoproto types, causing panics.
//
// Fix: Instead of letting modules register gateway handlers with clientCtx
// (which goes through the problematic Invoke path), we register them with a
// codecConn wrapper that explicitly adds ForceCodec(sdkCodec) to every gRPC
// call. This ensures gogoproto marshal/unmarshal is used end-to-end.
//
// This is a GENERIC fix — no per-endpoint URL mapping. Adding a new module
// requires one RegisterQueryHandlerClient line, and new endpoints within
// existing modules are automatically covered.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	gateway "github.com/cosmos/gogogateway"
	golangproto "github.com/golang/protobuf/proto"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/grpc"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/server/api"

	// SDK services
	cmtservice "github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	nodeservice "github.com/cosmos/cosmos-sdk/client/grpc/node"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"

	// SDK modules
	upgradetypes "cosmossdk.io/x/upgrade/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypesv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	// IBC modules
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	ibcclienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	ibcconnectiontypes "github.com/cosmos/ibc-go/v10/modules/core/03-connection/types"
	ibcchanneltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"

	// Custom modules
	blogtypes "sparkdream/x/blog/types"
	collecttypes "sparkdream/x/collect/types"
	commonstypes "sparkdream/x/commons/types"
	ecosystemtypes "sparkdream/x/ecosystem/types"
	forumtypes "sparkdream/x/forum/types"
	futarchytypes "sparkdream/x/futarchy/types"
	nametypes "sparkdream/x/name/types"
	reptypes "sparkdream/x/rep/types"
	revealtypes "sparkdream/x/reveal/types"
	seasontypes "sparkdream/x/season/types"
	sessiontypes "sparkdream/x/session/types"
	shieldtypes "sparkdream/x/shield/types"
	sparkdreamtypes "sparkdream/x/sparkdream/types"
	splittypes "sparkdream/x/split/types"
)

// codecConn wraps a gRPC connection and forces the SDK codec on every call.
// This bypasses the problematic proto v2 marshaling in the default gRPC codec.
type codecConn struct {
	inner    *grpc.ClientConn
	forceOpt grpc.CallOption
}

func (c *codecConn) Invoke(ctx context.Context, method string, args, reply interface{}, opts ...grpc.CallOption) error {
	return c.inner.Invoke(ctx, method, args, reply, append(opts, c.forceOpt)...)
}

func (c *codecConn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return c.inner.NewStream(ctx, desc, method, append(opts, c.forceOpt)...)
}

// registerGatewayRoutes registers all gRPC-gateway routes using our codecConn
// wrapper instead of clientCtx. This is the core fix — modules' gateway
// handlers will use the SDK codec for all gRPC calls.
func registerGatewayRoutes(mux *runtime.ServeMux, conn *codecConn) {
	ctx := context.Background()

	// SDK services
	cmtservice.RegisterServiceHandlerClient(ctx, mux, cmtservice.NewServiceClient(conn))
	txtypes.RegisterServiceHandlerClient(ctx, mux, txtypes.NewServiceClient(conn))
	nodeservice.RegisterServiceHandlerClient(ctx, mux, nodeservice.NewServiceClient(conn))

	// SDK modules
	authtypes.RegisterQueryHandlerClient(ctx, mux, authtypes.NewQueryClient(conn))
	banktypes.RegisterQueryHandlerClient(ctx, mux, banktypes.NewQueryClient(conn))
	consensustypes.RegisterQueryHandlerClient(ctx, mux, consensustypes.NewQueryClient(conn))
	distrtypes.RegisterQueryHandlerClient(ctx, mux, distrtypes.NewQueryClient(conn))
	govtypesv1.RegisterQueryHandlerClient(ctx, mux, govtypesv1.NewQueryClient(conn))
	minttypes.RegisterQueryHandlerClient(ctx, mux, minttypes.NewQueryClient(conn))
	slashingtypes.RegisterQueryHandlerClient(ctx, mux, slashingtypes.NewQueryClient(conn))
	stakingtypes.RegisterQueryHandlerClient(ctx, mux, stakingtypes.NewQueryClient(conn))
	upgradetypes.RegisterQueryHandlerClient(ctx, mux, upgradetypes.NewQueryClient(conn))

	// IBC modules
	ibctransfertypes.RegisterQueryHandlerClient(ctx, mux, ibctransfertypes.NewQueryClient(conn))
	ibcchanneltypes.RegisterQueryHandlerClient(ctx, mux, ibcchanneltypes.NewQueryClient(conn))
	ibcconnectiontypes.RegisterQueryHandlerClient(ctx, mux, ibcconnectiontypes.NewQueryClient(conn))
	ibcclienttypes.RegisterQueryHandlerClient(ctx, mux, ibcclienttypes.NewQueryClient(conn))

	// Custom modules — add one line here when adding a new module
	blogtypes.RegisterQueryHandlerClient(ctx, mux, blogtypes.NewQueryClient(conn))
	collecttypes.RegisterQueryHandlerClient(ctx, mux, collecttypes.NewQueryClient(conn))
	commonstypes.RegisterQueryHandlerClient(ctx, mux, commonstypes.NewQueryClient(conn))
	ecosystemtypes.RegisterQueryHandlerClient(ctx, mux, ecosystemtypes.NewQueryClient(conn))
	forumtypes.RegisterQueryHandlerClient(ctx, mux, forumtypes.NewQueryClient(conn))
	futarchytypes.RegisterQueryHandlerClient(ctx, mux, futarchytypes.NewQueryClient(conn))
	nametypes.RegisterQueryHandlerClient(ctx, mux, nametypes.NewQueryClient(conn))
	reptypes.RegisterQueryHandlerClient(ctx, mux, reptypes.NewQueryClient(conn))
	revealtypes.RegisterQueryHandlerClient(ctx, mux, revealtypes.NewQueryClient(conn))
	seasontypes.RegisterQueryHandlerClient(ctx, mux, seasontypes.NewQueryClient(conn))
	sessiontypes.RegisterQueryHandlerClient(ctx, mux, sessiontypes.NewQueryClient(conn))
	shieldtypes.RegisterQueryHandlerClient(ctx, mux, shieldtypes.NewQueryClient(conn))
	sparkdreamtypes.RegisterQueryHandlerClient(ctx, mux, sparkdreamtypes.NewQueryClient(conn))
	splittypes.RegisterQueryHandlerClient(ctx, mux, splittypes.NewQueryClient(conn))
}

// installGatewayFix sets up the codec-safe gateway routes and middleware.
func installGatewayFix(apiSvr *api.Server) {
	cdc := codec.NewProtoCodec(apiSvr.ClientCtx.InterfaceRegistry)

	conn := &codecConn{
		inner:    apiSvr.ClientCtx.GRPCClient,
		forceOpt: grpc.ForceCodec(cdc.GRPCCodec()),
	}

	// Create a FRESH mux — grpc-gateway v1 uses first-registered-wins,
	// so we can't override the SDK's routes on the existing mux.
	// Replicate the SDK's mux options from server/api/server.go:New().
	marshalerOption := &gateway.JSONPb{
		EmitDefaults: true,
		Indent:       "",
		OrigName:     true,
		AnyResolver:  apiSvr.ClientCtx.InterfaceRegistry,
	}
	newMux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, marshalerOption),
		runtime.WithProtoErrorHandler(runtime.DefaultHTTPProtoErrorHandler),
		runtime.WithIncomingHeaderMatcher(api.CustomGRPCHeaderMatcher),
		runtime.WithForwardResponseOption(func(ctx context.Context, w http.ResponseWriter, _ golangproto.Message) error {
			if meta, ok := runtime.ServerMetadataFromContext(ctx); ok {
				if vals := meta.HeaderMD.Get("x-cosmos-block-height"); len(vals) == 1 {
					w.Header().Set("x-cosmos-block-height", vals[0])
				}
			}
			return nil
		}),
	)

	// Register all gateway routes on the fresh mux using our codec wrapper
	registerGatewayRoutes(newMux, conn)

	// Replace the SDK's mux — Start() will mount this one
	apiSvr.GRPCGatewayRouter = newMux

	// Pre-intercept middleware for denom metadata defaults + panic safety net
	grpcConn := apiSvr.ClientCtx.GRPCClient
	apiSvr.Router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := strings.TrimPrefix(r.URL.Path, "/")

			// Pre-intercept: block endpoints (time.Time marshaling issue)
			// and denom metadata (default values when genesis has none)
			if bz, ok := preIntercept(cdc, grpcConn, path); ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(200)
				w.Write(bz)
				return
			}

			// Safety net: catch any unexpected panics
			defer func() {
				if rec := recover(); rec != nil {
					fmt.Fprintf(os.Stderr, "gateway_fix: handler panic for %s: %v\n", r.URL.Path, rec)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(500)
					fmt.Fprintf(w, `{"code":13,"message":"internal error"}`)
				}
			}()

			next.ServeHTTP(w, r)
		})
	})
}

// preIntercept handles endpoints that need to bypass the gateway marshaler:
// - Block endpoints: gogogateway JSONPb can't marshal time.Time (stdtime)
// - Denom metadata: provide defaults when genesis has none
func preIntercept(cdc *codec.ProtoCodec, conn *grpc.ClientConn, path string) ([]byte, bool) {
	if conn == nil {
		return nil, false
	}
	ctx := context.Background()

	// Block endpoints — time.Time fields panic in gogogateway marshaler
	if path == "cosmos/base/tendermint/v1beta1/blocks/latest" {
		client := cmtservice.NewServiceClient(conn)
		resp, err := client.GetLatestBlock(ctx, &cmtservice.GetLatestBlockRequest{})
		if err != nil {
			return nil, false
		}
		bz, err := cdc.MarshalJSON(resp)
		if err != nil {
			return nil, false
		}
		return bz, true
	}

	if strings.HasPrefix(path, "cosmos/base/tendermint/v1beta1/blocks/") {
		height := strings.TrimPrefix(path, "cosmos/base/tendermint/v1beta1/blocks/")
		if i := strings.IndexAny(height, "/?"); i >= 0 {
			height = height[:i]
		}
		var h int64
		fmt.Sscanf(height, "%d", &h)
		if h > 0 {
			client := cmtservice.NewServiceClient(conn)
			resp, err := client.GetBlockByHeight(ctx, &cmtservice.GetBlockByHeightRequest{Height: h})
			if err != nil {
				return nil, false
			}
			bz, err := cdc.MarshalJSON(resp)
			if err != nil {
				return nil, false
			}
			return bz, true
		}
	}

	// Denom metadata — provide defaults when genesis has none
	if path == "cosmos/bank/v1beta1/denoms_metadata" {
		client := banktypes.NewQueryClient(conn)
		resp, err := client.DenomsMetadata(ctx, &banktypes.QueryDenomsMetadataRequest{})
		if err != nil {
			return nil, false
		}
		if len(resp.Metadatas) == 0 {
			for _, m := range defaultMetadatas {
				resp.Metadatas = append(resp.Metadatas, m)
			}
		}
		bz, err := cdc.MarshalJSON(resp)
		if err != nil {
			return nil, false
		}
		return bz, true
	}

	if strings.HasPrefix(path, "cosmos/bank/v1beta1/denoms_metadata/") {
		denom := strings.TrimPrefix(path, "cosmos/bank/v1beta1/denoms_metadata/")
		if i := strings.IndexAny(denom, "/?"); i >= 0 {
			denom = denom[:i]
		}
		if meta, ok := defaultMetadatas[denom]; ok {
			resp := &banktypes.QueryDenomMetadataResponse{Metadata: meta}
			bz, err := cdc.MarshalJSON(resp)
			if err != nil {
				return nil, false
			}
			return bz, true
		}
	}

	return nil, false
}

var defaultMetadatas = map[string]banktypes.Metadata{
	"uspark": {
		Base: "uspark", Display: "spark", Name: "Spark", Symbol: "SPARK",
		Description: "The native staking token of Spark Dream.",
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: "uspark", Exponent: 0, Aliases: []string{"microspark"}},
			{Denom: "spark", Exponent: 6},
		},
	},
	"udream": {
		Base: "udream", Display: "dream", Name: "Dream", Symbol: "DREAM",
		Description: "Internal coordination token.",
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: "udream", Exponent: 0, Aliases: []string{"microdream"}},
			{Denom: "dream", Exponent: 6},
		},
	},
}
