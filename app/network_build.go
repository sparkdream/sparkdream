package app

import "fmt"

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
