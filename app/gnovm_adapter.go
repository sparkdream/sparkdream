package app

import (
	"context"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gnolang/gno/gnovm/pkg/gnolang"
	"github.com/gnolang/gno/gnovm/pkg/gnomod"
	gnovmmodulekeeper "github.com/sparkdream/gnovm/x/gnovm/keeper"

	artifacttypes "sparkdream/x/artifact/types"
)

// ArtifactGnoVMAdapter bridges the concrete x/gnovm keeper to the narrow
// artifacttypes.GnoVMKeeper renderers use (docs/x-artifact-spec.md §7.16).
// It holds the keeper by pointer because the VM inside it is built lazily on
// first use.
type ArtifactGnoVMAdapter struct {
	keeper *gnovmmodulekeeper.Keeper
}

// NewArtifactGnoVMAdapter wraps the app's gnovm keeper.
func NewArtifactGnoVMAdapter(k *gnovmmodulekeeper.Keeper) *ArtifactGnoVMAdapter {
	return &ArtifactGnoVMAdapter{keeper: k}
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

// EvalString runs expr read-only under the VM's query gas and allocation
// caps; VM panics surface as errors.
func (a *ArtifactGnoVMAdapter) EvalString(ctx context.Context, pkgPath, expr string) (string, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	gnoCtx, err := a.keeper.BuildGnoContext(sdkCtx)
	if err != nil {
		return "", err
	}
	return a.keeper.VMKeeper.QueryEvalString(gnoCtx, pkgPath, expr)
}
