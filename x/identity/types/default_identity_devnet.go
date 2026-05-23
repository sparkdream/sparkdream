//go:build devnet

package types

// DefaultChainIdentity for the devnet build (`-tags devnet`). Distinct
// denoms from mainnet and testnet so devnet SPARK / DREAM are
// unambiguously separate across IBC vouchers, indexers, and explorers.
// Operators running the canonical `sparkdream-dev-1` devnet build with
// this tag get a working federated genesis with no manual `genesis
// identity init` step.
//
// Build with: `go build -tags devnet ./...`
//
// The chain_human_name is "SparkdreamDev" (no space) so it passes the
// chain-id consistency check (§11.1) against the canonical
// `sparkdream-dev-1` chain_id: chainIDBase strips to "sparkdream",
// which is a substring of "sparkdreamdev" (lowercased), so the soft
// check passes without needing allow_chain_id_mismatch.
func DefaultChainIdentity() ChainIdentity {
	return ChainIdentity{
		ChainHumanName:       "SparkdreamDev",
		ChainTickerPrefix:    "SDD",
		BondDenom:            "uspark.sparkdreamdev",
		BondDisplaySymbol:    "SPARK",
		BondDisplayName:      "Sparkdream Dev Spark",
		BondDisplayDecimals:  6,
		DreamDenom:           "udream.sparkdreamdev",
		DreamDisplaySymbol:   "DREAM",
		DreamDisplayName:     "Sparkdream Dev Dream",
		DreamDisplayDecimals: 6,
		FoundedAt:            1735689600,
	}
}
