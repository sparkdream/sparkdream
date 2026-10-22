package types

import (
	"crypto/sha256"
	"encoding/hex"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"

	errorsmod "cosmossdk.io/errors"
)

// Renderer rules (docs/x-artifact-spec.md §7.16). The class names a pure
// Gno package whose exported
//
//	func Render(classID uint64, tokenID uint64, seed string, blockTime int64, height int64) string
//
// returns the displayed TokenMetadata as JSON. The package is checked once,
// at class creation, against the source x/gnovm has stored: a pure p/ path,
// not private, no realm imports, the expected Render signature, and a source
// hash equal to the one the creator signed.

const (
	// RendererFuncName is the entry point a renderer package exports.
	RendererFuncName = "Render"
	// MaxRenderOutputBytes caps what a render may return before parsing.
	MaxRenderOutputBytes = 64 * 1024
	// MaxRendererPathLength bounds Renderer.pkg_path.
	MaxRendererPathLength = 256
)

// GnoSourceFile is one stored source file of a Gno package.
type GnoSourceFile struct {
	Name string
	Body string
}

// GnoFuncSig is an exported function's signature as x/gnovm reports it.
type GnoFuncSig struct {
	Name    string
	Params  []string // type names
	Results []string // type names
}

// GnoPackage is what the artifact keeper learns about a stored package.
type GnoPackage struct {
	Files []GnoSourceFile
	// Pure: a published p/ package (never a realm, stdlib or run path).
	Pure bool
	// Private: gnomod.toml marks the module private (cannot be imported).
	Private bool
	Funcs   []GnoFuncSig
}

// RendererSourceHash is the lowercase hex SHA-256 over the package's .gno
// files sorted by name, each as name || 0x00 || body || 0x00. Module files
// (gnomod.toml, gno.mod) are left out: x/gnovm rewrites them at deployment
// with the creator, height and deposit, which a creator cannot know before
// the tx lands. The code that runs is what the hash pins.
func RendererSourceHash(files []GnoSourceFile) string {
	sorted := make([]GnoSourceFile, 0, len(files))
	for _, f := range files {
		if strings.HasSuffix(f.Name, ".gno") {
			sorted = append(sorted, f)
		}
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	h := sha256.New()
	for _, f := range sorted {
		h.Write([]byte(f.Name))
		h.Write([]byte{0})
		h.Write([]byte(f.Body))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// RendererImports lists the import paths of every .gno source file.
func RendererImports(files []GnoSourceFile) ([]string, error) {
	var out []string
	fset := token.NewFileSet()
	for _, f := range files {
		if !strings.HasSuffix(f.Name, ".gno") {
			continue
		}
		af, err := parser.ParseFile(fset, f.Name, f.Body, parser.ImportsOnly)
		if err != nil {
			return nil, errorsmod.Wrapf(ErrInvalidRenderer, "%s: %v", f.Name, err)
		}
		for _, imp := range af.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return nil, errorsmod.Wrapf(ErrInvalidRenderer, "%s: bad import %s", f.Name, imp.Path.Value)
			}
			out = append(out, path)
		}
	}
	return out, nil
}

// isRealmImport reports a <domain>/r/... (or ephemeral e/) import path.
func isRealmImport(path string) bool {
	parts := strings.SplitN(path, "/", 3)
	return len(parts) >= 2 && strings.Contains(parts[0], ".") && (parts[1] == "r" || parts[1] == "e")
}

var rendererParams = []string{"uint64", "uint64", "string", "int64", "int64"}

// ValidateRendererPackage applies the renderer rules to a stored package and
// the hash the creator supplied.
func ValidateRendererPackage(r Renderer, pkg GnoPackage) error {
	if !pkg.Pure {
		return errorsmod.Wrapf(ErrInvalidRenderer, "%s is not a pure p/ package", r.PkgPath)
	}
	if pkg.Private {
		return errorsmod.Wrapf(ErrInvalidRenderer, "%s is a private package", r.PkgPath)
	}
	if len(pkg.Files) == 0 {
		return errorsmod.Wrapf(ErrInvalidRenderer, "%s has no source files", r.PkgPath)
	}
	imports, err := RendererImports(pkg.Files)
	if err != nil {
		return err
	}
	for _, imp := range imports {
		if isRealmImport(imp) {
			return errorsmod.Wrapf(ErrInvalidRenderer, "%s imports realm %s", r.PkgPath, imp)
		}
	}
	if !hexHashPattern.MatchString(r.SourceHash) {
		return errorsmod.Wrap(ErrInvalidRenderer, "source_hash must be 64 lowercase hex characters")
	}
	if got := RendererSourceHash(pkg.Files); got != r.SourceHash {
		return errorsmod.Wrapf(ErrInvalidRenderer, "source_hash %s does not match stored package %s", r.SourceHash, got)
	}
	// state_version picks the entry point: Render, or RenderState with the
	// token's state document (render_state.go).
	name, params := RendererFuncName, rendererParams
	switch r.StateVersion {
	case 0:
	case RenderStateV1, RenderStateV2:
		name, params = RendererStateFuncName, rendererStateParams
	default:
		return errorsmod.Wrapf(ErrInvalidRenderer, "unknown state_version %d (max %d)", r.StateVersion, MaxRenderStateVersion)
	}
	for _, f := range pkg.Funcs {
		if f.Name != name {
			continue
		}
		if !equalStrings(f.Params, params) || !equalStrings(f.Results, []string{"string"}) {
			return errorsmod.Wrapf(ErrInvalidRenderer, "%s must be func %s(%s) string, got (%s) (%s)",
				name, name, strings.Join(params, ", "), strings.Join(f.Params, ", "), strings.Join(f.Results, ", "))
		}
		return nil
	}
	return errorsmod.Wrapf(ErrInvalidRenderer, "%s exports no %s", r.PkgPath, name)
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// RenderExpr is the Gno expression a renderer is evaluated with.
func RenderExpr(classID, tokenID uint64, seed string, blockTime, height int64) string {
	return RendererFuncName + "(" + strconv.FormatUint(classID, 10) + ", " + strconv.FormatUint(tokenID, 10) +
		", " + strconv.Quote(seed) + ", " + strconv.FormatInt(blockTime, 10) + ", " + strconv.FormatInt(height, 10) + ")"
}
