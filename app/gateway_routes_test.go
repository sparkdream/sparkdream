package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"cosmossdk.io/log"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/client"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// Guards the failure this file's registration loop was written to prevent: a
// module wired into the app whose REST routes never reach the gateway mux.
// That failure is silent — the build passes, the node boots, and the endpoint
// answers 501 — so it needs a test rather than review to catch.
//
// It bit us once already: an earlier hand-maintained registration list had
// dropped federation, guardian, identity and service, taking ~66 endpoints
// off the LCD without a single error anywhere.

func testApp(t *testing.T) *App {
	t.Helper()
	appOpts := make(simtestutil.AppOptionsMap)
	appOpts["home"] = t.TempDir()
	return New(log.NewNopLogger(), dbm.NewMemDB(), nil, true, appOpts)
}

// Every module the app wires in must be able to register gateway routes. The
// loop in registerGatewayRoutes skips anything failing this assertion, so a
// module that quietly stops implementing the method would lose its REST
// surface with nothing to show for it.
func TestEveryModuleRegistersGatewayRoutes(t *testing.T) {
	app := testApp(t)

	// The SDK's runtime module is app wiring, not a query surface — it has no
	// proto service and so no REST routes to register. Anything else landing
	// here is a module whose endpoints would silently 501.
	noRESTSurface := map[string]bool{"runtime": true}

	for name, mod := range app.ModuleManager.Modules {
		if noRESTSurface[name] {
			continue
		}
		if _, ok := mod.(hasGatewayRoutes); !ok {
			t.Errorf("module %q does not implement RegisterGRPCGatewayRoutes: "+
				"its REST endpoints will answer 501", name)
		}
	}
}

// End-to-end on the mux itself: an unregistered path is answerable only as
// 501, so asserting "not 501" on a real path proves the route was installed.
// The queries themselves fail (this client.Context has no node behind it),
// which is fine — we are testing routing, not query execution.
func TestGatewayMuxServesEveryModulePrefix(t *testing.T) {
	app := testApp(t)

	// The node's own mux, so unmatched routes answer 501 here exactly as they
	// do in production. A bare runtime.NewServeMux() answers 404 instead, and
	// the assertions below would pass without proving anything.
	mux := newGatewayMux(app.InterfaceRegistry())
	clientCtx := client.Context{}.WithCmdContext(context.Background()).
		WithInterfaceRegistry(app.InterfaceRegistry())
	registerGatewayRoutes(mux, clientCtx, app.ModuleManager)

	// One real declared path per custom module, including the four that the
	// old hand-maintained list had dropped.
	paths := []string{
		"/sparkdream/blog/v1/params",
		"/sparkdream/collect/v1/params",
		"/sparkdream/commons/v1/params",
		"/sparkdream/ecosystem/v1/params",
		"/sparkdream/federation/v1/params",
		"/sparkdream/forum/v1/params",
		"/sparkdream/futarchy/v1/params",
		"/sparkdream/guardian/v1/allowed_msgs",
		"/sparkdream/identity/v1/chain-identity",
		"/sparkdream/name/v1/params",
		"/sparkdream/rep/v1/params",
		"/sparkdream/reveal/v1/params",
		"/sparkdream/season/v1/params",
		"/sparkdream/service/v1/params",
		"/sparkdream/session/v1/params",
		"/sparkdream/shield/v1/params",
		"/sparkdream/split/v1/params",
		// SDK + IBC modules reach the mux through the same loop.
		"/cosmos/bank/v1beta1/params",
		"/cosmos/staking/v1beta1/params",
		"/ibc/core/channel/v1/channels",
		// Services registered outside the module manager.
		"/cosmos/base/tendermint/v1beta1/node_info",
		"/cosmos/base/node/v1beta1/config",
	}

	for _, p := range paths {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		require.NotEqual(t, http.StatusNotImplemented, rec.Code,
			"%s is not registered on the gateway mux", p)
	}
}

// The mux's marshaler is what makes REST responses look the way LCD consumers
// (explorers, Keplr, the e2e scripts' jq paths) expect: snake_case field names
// and zero-valued fields still present. It is also what keeps gogoproto custom
// types renderable at all — grpc-gateway's own JSONPb goes through proto v2
// reflection and panics on every math.Int/math.LegacyDec field.
//
// It is asserted AFTER registerGatewayRoutes, not on a freshly built mux,
// because a module can replace the marshaler during its own registration and
// one does (x/gnovm calls runtime.SetHTTPBodyMarshaler). Checking a fresh mux
// passes while every params endpoint on the live node answers 500.
func TestGatewayMuxMarshalerSurvivesModuleRegistration(t *testing.T) {
	app := testApp(t)
	mux := newGatewayMux(app.InterfaceRegistry())
	clientCtx := client.Context{}.WithCmdContext(context.Background()).
		WithInterfaceRegistry(app.InterfaceRegistry())
	registerGatewayRoutes(mux, clientCtx, app.ModuleManager)

	req := httptest.NewRequest(http.MethodGet, "/cosmos/bank/v1beta1/params", nil)
	_, outbound := runtime.MarshalerForRequest(mux, req)

	bz, err := outbound.Marshal(&banktypes.QueryParamsResponse{})
	require.NoError(t, err)

	// EmitDefaults: the zero-valued bool survives instead of being omitted.
	// OrigName: it is spelled default_send_enabled, not defaultSendEnabled.
	require.Contains(t, string(bz), `"default_send_enabled":false`,
		"gateway marshaler lost EmitDefaults/OrigName; REST field names and "+
			"zero values will not match what LCD clients expect")

	// The one that matters most: a response carrying a gogoproto custom type
	// must marshal at all. grpc-gateway's own JSONPb panics here rather than
	// returning an error, so this is asserted as "does not panic".
	require.NotPanics(t, func() {
		bz, err = outbound.Marshal(&stakingtypes.QueryParamsResponse{
			Params: stakingtypes.DefaultParams(),
		})
	}, "gateway marshaler panics on math.LegacyDec; every endpoint whose "+
		"response carries a gogoproto custom type will answer 500")
	require.NoError(t, err)
	require.Contains(t, string(bz), `"min_commission_rate"`)
}
