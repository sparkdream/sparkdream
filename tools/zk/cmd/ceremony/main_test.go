package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testBeacon1 = "00112233445566778899aabbccddeeff"
	testBeacon2 = "ffeeddccbbaa99887766554433221100"
)

// TestCeremony_EndToEnd runs a two-contributor ceremony on the real shield
// circuit, checks that verify accepts the sealed transcript, and then checks
// that it rejects a tampered key, a spliced-in Phase 2 contribution, and a
// srs_commons.bin that does not come from the Phase 1 transcript.
func TestCeremony_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("full MPC ceremony on the shield circuit")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "ceremony")

	for i := 0; i < 2; i++ {
		must(t, contributePhase1(dir))
	}
	must(t, sealPhase1(dir, testBeacon1))
	for i := 0; i < 2; i++ {
		must(t, contributePhase2(dir))
	}
	must(t, sealPhase2(dir, testBeacon2))
	must(t, verify(dir, ""))

	// A published key that is not the one the transcript produces.
	t.Run("tampered verifying key", func(t *testing.T) {
		bad := copyDir(t, dir, filepath.Join(root, "bad-vk"))
		path := filepath.Join(bad, vkFile)
		bz, err := os.ReadFile(path)
		must(t, err)
		bz[len(bz)/2] ^= 0x01
		must(t, os.WriteFile(path, bz, 0o644))
		if err := verify(bad, ""); err == nil {
			t.Fatal("verify must reject a modified verifying key")
		}
	})

	// A contribution made on the same sealed Phase 1 but in a separate run is a
	// well-formed Phase 2 on its own; the next contribution does not build on
	// it, so splicing it into the transcript must break the chain.
	t.Run("spliced phase 2 contribution", func(t *testing.T) {
		fork := filepath.Join(root, "fork")
		must(t, os.MkdirAll(fork, 0o755))
		for _, name := range []string{contributionName(phase1Prefix, 1), contributionName(phase1Prefix, 2), commonsFile, beacon1File} {
			copyFile(t, filepath.Join(dir, name), filepath.Join(fork, name))
		}
		must(t, contributePhase2(fork))

		bad := copyDir(t, dir, filepath.Join(root, "bad-phase2"))
		name := contributionName(phase2Prefix, 1)
		copyFile(t, filepath.Join(fork, name), filepath.Join(bad, name))
		if err := verify(bad, ""); err == nil {
			t.Fatal("verify must reject a transcript with a foreign phase 2 contribution")
		}
	})

	// Phase 2 sealed on commons that the Phase 1 transcript does not produce
	// would yield keys nobody can audit.
	t.Run("foreign srs commons", func(t *testing.T) {
		other := filepath.Join(root, "other")
		must(t, contributePhase1(other))
		must(t, sealPhase1(other, testBeacon1))

		bad := filepath.Join(root, "bad-commons")
		must(t, os.MkdirAll(bad, 0o755))
		for _, name := range []string{
			contributionName(phase1Prefix, 1), contributionName(phase1Prefix, 2), beacon1File,
			contributionName(phase2Prefix, 1), contributionName(phase2Prefix, 2),
		} {
			copyFile(t, filepath.Join(dir, name), filepath.Join(bad, name))
		}
		copyFile(t, filepath.Join(other, commonsFile), filepath.Join(bad, commonsFile))
		err := sealPhase2(bad, testBeacon2)
		if err == nil {
			t.Fatal("seal-phase2 must reject a srs_commons.bin the phase 1 transcript does not produce")
		}
		if !strings.Contains(err.Error(), commonsFile) {
			t.Fatalf("seal-phase2 failed for the wrong reason: %v", err)
		}
	})
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	bz, err := os.ReadFile(src)
	must(t, err)
	must(t, os.WriteFile(dst, bz, 0o644))
}

func copyDir(t *testing.T, src, dst string) string {
	t.Helper()
	must(t, os.MkdirAll(dst, 0o755))
	entries, err := os.ReadDir(src)
	must(t, err)
	for _, e := range entries {
		copyFile(t, filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()))
	}
	return dst
}
