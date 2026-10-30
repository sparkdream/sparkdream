package app

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLateWiredKeepersReachTheAppModule: when an AppModule holds its keeper
// by value, a Set*Keeper call in app.go after depinject only changes app.go's
// copy unless the setter writes through a shared pointer. The message and
// query servers then see nil (x/blog's commons keeper, x/forum's season
// keeper and x/season's content keepers were all silently nil). Every
// setter on such a keeper must write into its `late` pointer struct.
func TestLateWiredKeepersReachTheAppModule(t *testing.T) {
	valueKeeper := regexp.MustCompile(`(?m)^\s+keeper\s+keeper\.Keeper\b`)
	setter := regexp.MustCompile(`func \(k \*?Keeper\) (Set\w+)\([^)]*\)\s*\{\n((?:.*\n){1,4})`)
	direct := regexp.MustCompile(`(?m)^\s*k\.(\w+)\s*=`)
	modules, err := filepath.Glob(filepath.Join("..", "x", "*", "module", "module.go"))
	require.NoError(t, err)
	require.NotEmpty(t, modules)
	for _, mf := range modules {
		src, err := os.ReadFile(mf)
		require.NoError(t, err)
		if !valueKeeper.Match(src) {
			continue
		}
		keeperDir := filepath.Join(filepath.Dir(filepath.Dir(mf)), "keeper")
		files, err := filepath.Glob(filepath.Join(keeperDir, "*.go"))
		require.NoError(t, err)
		for _, f := range files {
			if filepath.Ext(f) != ".go" || regexp.MustCompile(`_test\.go$`).MatchString(f) {
				continue
			}
			b, err := os.ReadFile(f)
			require.NoError(t, err)
			for _, m := range setter.FindAllSubmatch(b, -1) {
				for _, d := range direct.FindAllSubmatch(m[2], -1) {
					require.Equal(t, "late", string(d[1]),
						"%s: %s assigns k.%s directly; the AppModule holds this keeper by value, so write it through the shared k.late pointer", f, m[1], d[1])
				}
			}
		}
	}
}
