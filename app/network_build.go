package app

import (
	"fmt"
	"os"
)

// canonicalNetworkChainIDs maps each canonical network's chain-id to the build
// tag its binary must carry. Several values are compiled per build tag
// (params_vals_*, genesis_vals_*, x/shield's verification-key requirement,
// x/artifact's hard bounds): a node built for another network would compute
// different results and fork itself off, and an untagged test build skips
// x/shield proof verification entirely. Sister chains with their own chain-ids
// are not listed and run whichever network build they choose.
var canonicalNetworkChainIDs = map[string]string{
	"sparkdream-1":      "mainnet",
	"sparkdream-test-1": "testnet",
	"sparkdream-dev-1":  "devnet",
}

// checkNetworkBuild refuses to run a binary on a canonical network it was not
// built for.
func checkNetworkBuild(chainID string) error {
	want, canonical := canonicalNetworkChainIDs[chainID]
	if !canonical || want == builtForNetwork {
		return nil
	}
	got := builtForNetwork
	if got == "" {
		got = "untagged test build"
	}
	return fmt.Errorf("chain-id %s requires a binary built with -tags %s, this one is %s (make build-%s)", chainID, want, got, want)
}

// AllowTestBuildEnv opts an untagged test build into starting a node on a
// chain-id outside localTestChainIDs (multichain and replay e2e chains).
const AllowTestBuildEnv = "SPARKDREAM_ALLOW_TEST_BUILD"

// localTestChainIDs are the chain-ids an untagged test build may start a node
// on without opting in: the empty chain-id of in-process tests and the local
// ignite / e2e chain.
var localTestChainIDs = map[string]bool{
	"":           true,
	"sparkdream": true,
}

// CheckTestBuildStart refuses to start a node from an untagged test build on a
// chain-id that is not a known local test chain, unless AllowTestBuildEnv=1.
// A sister chain launched from a plain `go build` / `make build` would run test
// economics and windows and skip x/shield proof verification; a log line is
// too easy to miss, so the daemon stops instead. Only the daemon's app creator
// calls this: in-process tests and simulations build the app directly.
func CheckTestBuildStart(chainID string) error {
	if builtForNetwork != "" || localTestChainIDs[chainID] || os.Getenv(AllowTestBuildEnv) == "1" {
		return nil
	}
	return fmt.Errorf("%s Set %s=1 to run a local test chain under this chain-id anyway.",
		testBuildWarning(chainID), AllowTestBuildEnv)
}

// testBuildWarning returns a non-empty warning when an untagged test build
// starts on a chain-id it cannot verify is a local test chain. A test build
// carries test economics and windows and skips x/shield proof verification
// when no verification key is registered, so a sister chain launched from a
// plain `go build` / `make build` must notice. Not consensus: it only logs.
func testBuildWarning(chainID string) string {
	if builtForNetwork != "" || chainID == "" {
		return ""
	}
	return fmt.Sprintf("UNTAGGED TEST BUILD on chain-id %q: test parameters are active and x/shield "+
		"skips proof verification without a registered key. A real network must run a binary built "+
		"with -tags mainnet, testnet or devnet.", chainID)
}
