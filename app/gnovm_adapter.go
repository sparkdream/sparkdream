package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gnolang/gno/gnovm/pkg/gnolang"
	"github.com/gnolang/gno/gnovm/pkg/gnomod"
	gnovmmodulekeeper "github.com/sparkdream/gnovm/x/gnovm/keeper"
	gnovmtypes "github.com/sparkdream/gnovm/x/gnovm/types"

	artifacttypes "sparkdream/x/artifact/types"
)

// ArtifactGnoVMAdapter bridges the concrete x/gnovm keeper to the narrow
// artifacttypes.GnoVMKeeper renderers use (docs/x-artifact-spec.md §7.16).
// It holds the keeper by pointer because the VM inside it is built lazily on
// first use.
//
// Package reads only persisted state (files, docs), so the app's keeper copy
// serves it. Eval must run on the gnovm module's own VM: Gno keeps
// preprocessed block nodes in the VM's memory only (they are rebuilt at VM
// initialisation, never persisted), so a second VM built before a package
// was deployed cannot evaluate that package. The module's VM is the one that
// executes deploys, and the only handle on it is its query service.
type ArtifactGnoVMAdapter struct {
	keeper *gnovmmodulekeeper.Keeper
	// queries resolves the app's query router, which exists only once the
	// BaseApp is built (after keepers are wired).
	queries func() *baseapp.GRPCQueryRouter
}

// gnovmEvalRoute is x/gnovm's Query/Eval method.
const gnovmEvalRoute = "/gnovm.gnovm.v1.Query/Eval"

// NewArtifactGnoVMAdapter wraps the app's gnovm keeper and query router.
func NewArtifactGnoVMAdapter(k *gnovmmodulekeeper.Keeper, queries func() *baseapp.GRPCQueryRouter) *ArtifactGnoVMAdapter {
	return &ArtifactGnoVMAdapter{keeper: k, queries: queries}
}

var _ artifacttypes.GnoVMKeeper = (*ArtifactGnoVMAdapter)(nil)

// Package reads a stored package's files and exported function signatures
// through the VM's query surface (gas-metered by the caller's context).
func (a *ArtifactGnoVMAdapter) Package(ctx context.Context, pkgPath string) (artifacttypes.GnoPackage, bool, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	gnoCtx, err := a.keeper.BuildGnoContext(sdkCtx)
	if err != nil {
		return artifacttypes.GnoPackage{}, false, err
	}
	listing, err := a.keeper.VMKeeper.QueryFile(gnoCtx, pkgPath)
	if err != nil {
		// vm.InvalidPackageError: the path is not stored.
		return artifacttypes.GnoPackage{}, false, nil
	}
	pkg := artifacttypes.GnoPackage{Pure: gnolang.IsPPackagePath(pkgPath)}
	for _, name := range strings.Split(listing, "\n") {
		if name == "" {
			continue
		}
		body, err := a.keeper.VMKeeper.QueryFile(gnoCtx, pkgPath+"/"+name)
		if err != nil {
			return artifacttypes.GnoPackage{}, false, err
		}
		pkg.Files = append(pkg.Files, artifacttypes.GnoSourceFile{Name: name, Body: body})
		if name == "gnomod.toml" || name == "gno.mod" {
			if mod, err := gnomod.ParseBytes(name, []byte(body)); err == nil {
				pkg.Private = mod.Private
			}
		}
	}
	// QueryFuncs serves realms only; QueryDoc documents pure packages too and
	// carries the same typed signatures (methods have a receiver Type).
	doc, err := a.keeper.VMKeeper.QueryDoc(gnoCtx, pkgPath)
	if err != nil {
		return artifacttypes.GnoPackage{}, false, err
	}
	for _, f := range doc.Funcs {
		if f.Type != "" {
			continue
		}
		sig := artifacttypes.GnoFuncSig{Name: f.Name}
		for _, p := range f.Params {
			sig.Params = append(sig.Params, p.Type)
		}
		for _, r := range f.Results {
			sig.Results = append(sig.Results, r.Type)
		}
		pkg.Funcs = append(pkg.Funcs, sig)
	}
	return pkg, true, nil
}

// EvalString runs expr read-only on the gnovm module's VM, under the VM's
// query gas and allocation caps; VM panics surface as errors. The expression
// must yield exactly one string.
func (a *ArtifactGnoVMAdapter) EvalString(ctx context.Context, pkgPath, expr string) (string, error) {
	var handler baseapp.GRPCQueryHandler
	if router := a.queries(); router != nil {
		handler = router.Route(gnovmEvalRoute)
	}
	if handler == nil {
		return "", errors.New("gnovm eval query is not registered")
	}
	bz, err := (&gnovmtypes.QueryEvalRequest{PkgPath: pkgPath, Expr: expr}).Marshal()
	if err != nil {
		return "", err
	}
	res, err := handler(sdk.UnwrapSDKContext(ctx), &abci.RequestQuery{Path: gnovmEvalRoute, Data: bz})
	if err != nil {
		return "", err
	}
	var out gnovmtypes.QueryEvalResponse
	if err := out.Unmarshal(res.Value); err != nil {
		return "", err
	}
	return parseGnoString(out.Result)
}

// parseGnoString reads the one string result QueryEval prints as
// `("<Go-quoted string>" string)`; anything else (several results, another
// type) is an error.
func parseGnoString(result string) (string, error) {
	inner, ok := strings.CutPrefix(result, "(")
	if ok {
		inner, ok = strings.CutSuffix(inner, " string)")
	}
	if !ok {
		return "", fmt.Errorf("expected 1 string result, got %.80q", result)
	}
	s, err := strconv.Unquote(inner)
	if err != nil {
		return "", fmt.Errorf("expected 1 string result, got %.80q", result)
	}
	return s, nil
}
