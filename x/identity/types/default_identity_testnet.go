//go:build testnet && !devnet

package types

// DefaultChainIdentity for the testnet build (`-tags testnet`). Distinct
// denoms from mainnet so testnet SPARK / DREAM are unambiguously separate
// from mainnet across IBC vouchers, indexers, and explorers. Operators
// running the canonical `sparkdream-test-1` testnet build with this tag
// get a working federated genesis with no manual `genesis identity init`
// step.
//
// Build with: `go build -tags testnet ./...`
//
// The chain_human_name is "SparkdreamTest" (no space) so it passes the
// chain-id consistency check (§11.1) against the canonical
// `sparkdream-test-1` chain_id: chainIDBase strips to "sparkdream",
// which is a substring of "sparkdreamtest" (lowercased), so the soft
// check passes without needing allow_chain_id_mismatch.
func DefaultChainIdentity() ChainIdentity {
	return ChainIdentity{
		ChainHumanName:       "SparkdreamTest",
		ChainTickerPrefix:    "SDT",
		BondDenom:            "uspark.sparkdreamtest",
		BondDisplaySymbol:    "SPARK",
		BondDisplayName:      "Sparkdream Test Spark",
		BondDisplayDecimals:  6,
		DreamDenom:           "udream.sparkdreamtest",
		DreamDisplaySymbol:   "DREAM",
		DreamDisplayName:     "Sparkdream Test Dream",
		DreamDisplayDecimals: 6,
		FoundedAt:            1735689600,
	}
}
