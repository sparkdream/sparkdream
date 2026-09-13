package app

// gateway_fix.go re-registers the REST gateway routes on a mux we control.
//
// Why it exists: the gogogateway JSONPb marshaler cannot render the
// *time.Time fields on the block endpoints, so those are answered ahead of
// the mux by preIntercept below. Rebuilding the mux is what lets us install
// that middleware and our own marshaler/error-handler options — grpc-gateway
// v1 is first-registered-wins, so the SDK's mux cannot be amended in place
// and has to be replaced wholesale.
//
// Registration mirrors runtime.App.RegisterAPIRoutes exactly: the three
// non-module SDK services, then every module's own RegisterGRPCGatewayRoutes.
// Modules are read from the module manager rather than a list kept here, so a
// new module's REST routes appear the moment it is wired into the app. An
// earlier revision of this file did keep such a list, and four modules
// (federation, guardian, identity, service) were silently missing from it —
// nothing fails at build or boot, the endpoints just answer 501.
//
// Marshaler safety: calling the modules' own RegisterGRPCGatewayRoutes lets a
// module reach into the mux, and x/gnovm does — it calls
// runtime.SetHTTPBodyMarshaler, replacing the wildcard marshaler with
// grpc-gateway's proto v2-backed JSONPb, which panics on gogoproto custom
// types across every route. registerGatewayRoutes therefore re-asserts our
// marshaler after the module loop; see setGatewayMarshaler.
//
// Codec safety: these handlers query through clientCtx, whose Invoke either
// calls clientCtx.GRPCClient — which the SDK dials with
// grpc.ForceCodec(gogo) as a default call option (server/start.go) — or,
// when gRPC is disabled and that client is nil, falls back to an ABCI query
// marshaled with ctx.gRPCCodec(). Both paths use the SDK's gogoproto codec,
// which is what keeps proto v2 reflection away from gogoproto custom types
// (math.Int, math.LegacyDec).

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	gateway "github.com/cosmos/gogogateway"
	golangproto "github.com/golang/protobuf/proto"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/grpc"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/server/api"
	"github.com/cosmos/cosmos-sdk/types/module"

	// Services the SDK registers outside the module manager, so the loop over
	// modules cannot reach them.
	cmtservice "github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	nodeservice "github.com/cosmos/cosmos-sdk/client/grpc/node"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
)

// hasGatewayRoutes is the one method of module.AppModuleBasic we need. Kept
// narrow deliberately: it matches any module that can register REST routes,
// without requiring the rest of the basic-module surface.
type hasGatewayRoutes interface {
	RegisterGRPCGatewayRoutes(client.Context, *runtime.ServeMux)
}

// registerGatewayRoutes registers every REST route on the given mux, in the
// same order runtime.App.RegisterAPIRoutes does: the SDK services that live
// outside the module manager first, then each module's own registration.
//
// Module order is sorted rather than map order so registration is
// deterministic; grpc-gateway v1 is first-registered-wins, and a random order
// would resolve any future path collision differently from run to run.
func registerGatewayRoutes(mux *runtime.ServeMux, clientCtx client.Context, mm *module.Manager) {
	authtx.RegisterGRPCGatewayRoutes(clientCtx, mux)
	cmtservice.RegisterGRPCGatewayRoutes(clientCtx, mux)
	nodeservice.RegisterGRPCGatewayRoutes(clientCtx, mux)

	names := make([]string, 0, len(mm.Modules))
	for name := range mm.Modules {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if gw, ok := mm.Modules[name].(hasGatewayRoutes); ok {
			gw.RegisterGRPCGatewayRoutes(clientCtx, mux)
		}
	}

	// Re-assert our marshaler, because a module is free to replace it during
	// its own registration and one does: x/gnovm calls
	// runtime.SetHTTPBodyMarshaler, which overwrites the wildcard entry with
	// grpc-gateway's own JSONPb. That one marshals through
	// github.com/golang/protobuf/jsonpb and so hits proto v2 reflection,
	// which panics on every gogoproto custom type (math.Int, math.LegacyDec)
	// — the exact failure this file exists to prevent. The marshaler is a
	// single map entry consulted per request, so one module clobbering it
	// breaks every route, not just its own.
	//
	// This must run AFTER the module loop. Doing it in newGatewayMux alone is
	// not enough, and looks fine in any test that checks a freshly built mux.
	setGatewayMarshaler(mux, clientCtx.InterfaceRegistry)
}

// setGatewayMarshaler installs our gogoproto-aware marshaler as the mux's
// wildcard marshaler. ServeMuxOption is just func(*ServeMux), so the same
// option the constructor takes can be re-applied to a live mux.
//
// It is wrapped in grpc-gateway's HTTPBodyMarshaler so that a handler
// returning google.api.HttpBody still renders as raw bytes — that is what
// x/gnovm wanted from SetHTTPBodyMarshaler, and it is preserved here rather
// than traded away. Everything that is not an HttpBody falls through to the
// gogo marshaler.
func setGatewayMarshaler(mux *runtime.ServeMux, ir codectypes.InterfaceRegistry) {
	m := &runtime.HTTPBodyMarshaler{
		Marshaler: &gateway.JSONPb{
			EmitDefaults: true,
			Indent:       "",
			OrigName:     true,
			AnyResolver:  ir,
		},
	}
	runtime.WithMarshalerOption(runtime.MIMEWildcard, m)(mux)
}

// newGatewayMux builds the replacement mux, replicating the SDK's own options
// from server/api/server.go:New(). Shared with the tests so they exercise the
// same routing behaviour the node serves — notably the proto error handler,
// which is what makes an unmatched route answer 501 rather than 404.
func newGatewayMux(ir codectypes.InterfaceRegistry) *runtime.ServeMux {
	mux := runtime.NewServeMux(
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
	setGatewayMarshaler(mux, ir)
	return mux
}

// installGatewayFix sets up the gateway routes and middleware.
func installGatewayFix(apiSvr *api.Server, mm *module.Manager) {
	cdc := codec.NewProtoCodec(apiSvr.ClientCtx.InterfaceRegistry)

	// Create a FRESH mux — grpc-gateway v1 uses first-registered-wins,
	// so we can't override the SDK's routes on the existing mux.
	newMux := newGatewayMux(apiSvr.ClientCtx.InterfaceRegistry)

	// Register all gateway routes on the fresh mux
	registerGatewayRoutes(newMux, apiSvr.ClientCtx, mm)

	// Replace the SDK's mux — Start() will mount this one
	apiSvr.GRPCGatewayRouter = newMux

	// Pre-intercept middleware for the block endpoints + panic safety net
	grpcConn := apiSvr.ClientCtx.GRPCClient
	apiSvr.Router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := strings.TrimPrefix(r.URL.Path, "/")

			// Pre-intercept: block endpoints (time.Time marshaling issue)
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

// preIntercept answers the endpoints that have to bypass the gateway
// marshaler: the block endpoints, whose *time.Time (stdtime) fields the
// gogogateway JSONPb marshaler cannot render. Everything else falls through
// to the mux.
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

	return nil, false
}
