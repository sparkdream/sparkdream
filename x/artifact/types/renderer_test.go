package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/types"
)

const rendererSrc = `package aurora

import (
	"strconv"
	"gno.land/p/demo/ufmt"
)

func Render(classID uint64, tokenID uint64, seed string, blockTime int64, height int64) string {
	return ufmt.Sprintf("{\"name\":\"aurora #%s\"}", strconv.Itoa(int(tokenID)))
}
`

func goodPackage() types.GnoPackage {
	return types.GnoPackage{
		Files: []types.GnoSourceFile{{Name: "aurora.gno", Body: rendererSrc}, {Name: "gnomod.toml", Body: "module = \"gno.land/p/demo/aurora\"\n"}},
		Pure:  true,
		Funcs: []types.GnoFuncSig{{Name: "Render", Params: []string{"uint64", "uint64", "string", "int64", "int64"}, Results: []string{"string"}}},
	}
}

func TestRendererSourceHashIsOrderIndependent(t *testing.T) {
	pkg := goodPackage()
	h1 := types.RendererSourceHash(pkg.Files)
	h2 := types.RendererSourceHash([]types.GnoSourceFile{pkg.Files[1], pkg.Files[0]})
	require.Equal(t, h1, h2)
	require.Len(t, h1, 64)
	// Module files are not part of the hash: gnovm rewrites them on deploy.
	pkg.Files[1].Body += "\n[addpkg]\nheight = 42\n"
	require.Equal(t, h1, types.RendererSourceHash(pkg.Files))
	pkg.Files[0].Body += "\n// changed"
	require.NotEqual(t, h1, types.RendererSourceHash(pkg.Files))
}

func TestValidateRendererPackage(t *testing.T) {
	good := goodPackage()
	r := types.Renderer{PkgPath: "gno.land/p/demo/aurora", SourceHash: types.RendererSourceHash(good.Files)}
	require.NoError(t, types.ValidateRendererPackage(r, good))

	cases := []struct {
		name string
		mut  func(*types.Renderer, *types.GnoPackage)
		want string
	}{
		{"realm path", func(_ *types.Renderer, p *types.GnoPackage) { p.Pure = false }, "pure"},
		{"private", func(_ *types.Renderer, p *types.GnoPackage) { p.Private = true }, "private"},
		{"no files", func(_ *types.Renderer, p *types.GnoPackage) { p.Files = nil }, "no source"},
		{"realm import", func(_ *types.Renderer, p *types.GnoPackage) {
			p.Files[0].Body = "package aurora\nimport \"gno.land/r/demo/counter\"\nfunc Render(a uint64, b uint64, c string, d int64, e int64) string { return \"\" }\n"
			p.Files = p.Files[:1]
		}, "imports realm"},
		{"hash mismatch", func(r *types.Renderer, _ *types.GnoPackage) { r.SourceHash = types.SeedCommitment([]byte("x")) }, "does not match"},
		{"bad hash", func(r *types.Renderer, _ *types.GnoPackage) { r.SourceHash = "abc" }, "64 lowercase"},
		{"no render", func(_ *types.Renderer, p *types.GnoPackage) { p.Funcs = nil }, "exports no Render"},
		{"wrong signature", func(_ *types.Renderer, p *types.GnoPackage) {
			p.Funcs = []types.GnoFuncSig{{Name: "Render", Params: []string{"string"}, Results: []string{"string"}}}
		}, "must be func Render"},
		{"unparsable source", func(r *types.Renderer, p *types.GnoPackage) {
			p.Files[0].Body = "package aurora\nimport ("
			r.SourceHash = types.RendererSourceHash(p.Files)
		}, "aurora.gno"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr, pkg := r, goodPackage()
			tc.mut(&rr, &pkg)
			if tc.name != "hash mismatch" && tc.name != "bad hash" && tc.name != "unparsable source" {
				rr.SourceHash = types.RendererSourceHash(pkg.Files)
			}
			err := types.ValidateRendererPackage(rr, pkg)
			require.ErrorIs(t, err, types.ErrInvalidRenderer)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestRenderExpr(t *testing.T) {
	require.Equal(t, `Render(3, 42, "ab", 1700000000, 100)`, types.RenderExpr(3, 42, "ab", 1_700_000_000, 100))
}
