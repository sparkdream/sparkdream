// Package docker_test guards the allowlisted build context of the sdap image.
package docker_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const modulePath = "sparkdream"

// goDirRoots are the top-level directories that hold this module's Go
// packages. An allowlist entry under one of them is expected to be a package
// the daemons import; entries elsewhere (go.mod, the entrypoint script) are
// not checked for staleness.
var goDirRoots = []string{"cmd/", "internal/", "tools/", "x/", "app/"}

// TestSdapBuildContextCoversDaemonImports fails when a daemon built by
// Dockerfile-sdap imports a package of this module that
// Dockerfile-sdap.dockerignore does not send to the build. The image then
// fails to build with "package ... is not in std", which nothing else catches
// before a release (v1.1.1: x/storage/types and tools/fileprofile).
func TestSdapBuildContextCoversDaemonImports(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	dockerfile, err := os.ReadFile("Dockerfile-sdap")
	require.NoError(t, err)
	var targets []string
	for _, m := range regexp.MustCompile(`go build [^\n]*?(\./cmd/[\w-]+)`).FindAllStringSubmatch(string(dockerfile), -1) {
		targets = append(targets, m[1])
	}
	require.NotEmpty(t, targets, "no `go build ./cmd/...` line found in Dockerfile-sdap")

	ignore, err := os.ReadFile("Dockerfile-sdap.dockerignore")
	require.NoError(t, err)
	var allowed []string
	for _, line := range strings.Split(string(ignore), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "!") {
			allowed = append(allowed, strings.TrimSuffix(strings.TrimPrefix(line, "!"), "/"))
		}
	}
	require.NotEmpty(t, allowed, "no allowlist entries found in Dockerfile-sdap.dockerignore")

	// the image builds with CGO_ENABLED=0 on linux; list the same build
	cmd := exec.Command("go", append([]string{"list", "-deps"}, targets...)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux")
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			stderr = string(ee.Stderr)
		}
		require.NoError(t, err, "go list -deps %v: %s", targets, stderr)
	}

	covered := func(dir string) (string, bool) {
		for _, a := range allowed {
			if dir == a || strings.HasPrefix(dir, a+"/") {
				return a, true
			}
		}
		return "", false
	}

	used := map[string]bool{}
	var missing []string
	for _, pkg := range strings.Fields(string(out)) {
		if !strings.HasPrefix(pkg, modulePath+"/") {
			continue
		}
		dir := strings.TrimPrefix(pkg, modulePath+"/")
		entry, ok := covered(dir)
		if !ok {
			missing = append(missing, "!"+dir)
			continue
		}
		used[entry] = true
	}
	sort.Strings(missing)
	require.Empty(t, missing,
		"Dockerfile-sdap.dockerignore does not send packages the sdap daemons import; add these lines")

	var stale []string
	for _, a := range allowed {
		if used[a] {
			continue
		}
		for _, r := range goDirRoots {
			if strings.HasPrefix(a, r) {
				stale = append(stale, "!"+a)
				break
			}
		}
	}
	require.Empty(t, stale,
		"Dockerfile-sdap.dockerignore sends packages no sdap daemon imports any more; remove these lines")
}
