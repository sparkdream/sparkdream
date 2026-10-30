package app

import (
	"context"
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/baseapp"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	gnovmtypes "github.com/sparkdream/gnovm/x/gnovm/types"
	"github.com/stretchr/testify/require"
)

func TestParseGnoString(t *testing.T) {
	doc := `{"name":"Zenith #1","description":"a \"quoted\" (paren) string)"}`
	got, err := parseGnoString("(" + strconv.Quote(doc) + " string)")
	require.NoError(t, err)
	require.Equal(t, doc, got)

	got, err = parseGnoString(`("" string)`)
	require.NoError(t, err)
	require.Equal(t, "", got)

	for _, bad := range []string{
		``,
		`(42 int)`,
		`("a" string)` + "\n" + `("b" string)`,
		`("unterminated string)`,
		`"bare"`,
	} {
		_, err := parseGnoString(bad)
		require.Error(t, err, bad)
	}
}

// Besides the per-call gas budget, the adapter caches each result for its
// height and refuses to run more than maxConcurrentRenders at once (the
// Token query then falls back to stored metadata).
func TestRenderEvalCacheAndConcurrencyBound(t *testing.T) {
	evals := 0
	a := NewArtifactGnoVMAdapter(nil, func() *baseapp.GRPCQueryRouter {
		evals++
		return nil // every eval fails, which is cached like a result
	})
	ctx := sdk.Context{}.WithBlockHeight(7)
	_, err := a.EvalString(ctx, "gno.land/p/zenith/render", "Render(1)")
	require.Error(t, err)
	_, err = a.EvalString(ctx, "gno.land/p/zenith/render", "Render(1)")
	require.Error(t, err)
	require.Equal(t, 1, evals, "the second query at the same height is served from the cache")

	_, _ = a.EvalString(ctx.WithBlockHeight(8), "gno.land/p/zenith/render", "Render(1)")
	require.Equal(t, 2, evals, "a new height evaluates again")

	for i := 0; i < maxConcurrentRenders; i++ {
		a.slots <- struct{}{}
	}
	_, err = a.EvalString(ctx.WithBlockHeight(8), "gno.land/p/zenith/render", "Render(2)")
	require.ErrorContains(t, err, "busy")
	require.Equal(t, 2, evals, "no evaluation ran while every slot was taken")
}

// evalRecorder is a gnovm query server that records the Eval request.
type evalRecorder struct {
	gnovmtypes.UnimplementedQueryServer
	req *gnovmtypes.QueryEvalRequest
}

func (r *evalRecorder) Eval(_ context.Context, req *gnovmtypes.QueryEvalRequest) (*gnovmtypes.QueryEvalResponse, error) {
	r.req = req
	return &gnovmtypes.QueryEvalResponse{Result: `("ok" string)`}, nil
}

// ART-09: every renderer evaluation asks gnovm for renderMaxGas instead of
// the VM's 3B query cap (gnovm enforces it; see its TestQueryEvalBounded).
func TestRenderEvalSendsGasBudget(t *testing.T) {
	rec := &evalRecorder{}
	router := baseapp.NewGRPCQueryRouter()
	router.SetInterfaceRegistry(codectypes.NewInterfaceRegistry())
	gnovmtypes.RegisterQueryServer(router, rec)

	a := NewArtifactGnoVMAdapter(nil, func() *baseapp.GRPCQueryRouter { return router })
	out, err := a.EvalString(sdk.Context{}.WithBlockHeight(3), "gno.land/p/zenith/render", "Render(1)")
	require.NoError(t, err)
	require.Equal(t, "ok", out)
	require.NotNil(t, rec.req)
	require.Equal(t, uint64(renderMaxGas), rec.req.MaxGas)
	require.Equal(t, "Render(1)", rec.req.Expr)
}
