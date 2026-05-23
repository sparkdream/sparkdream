//go:build !testnet && !devnet

package types

// DefaultChainIdentity returns the chain identity baked into this binary.
// Selected at compile time via build tag:
//
//   - Default (no tag): the Sparkdream chain identity below.
//   - `//go:build testnet`: the sparkdream-test-1 identity (separate file).
//
// For federated deployments other than the canonical Sparkdream chain
// (Phoenix, Aurora, ...), build the binary with custom values:
//   - either fork this file in a vendored build,
//   - or use `-ldflags` to override these constants (requires changing
//     the consts to vars; not done in v1).
//
// Operators who want to override at genesis-construction time use
// `sparkdreamd genesis identity init --force --i-mean-it ...` after
// `sparkdreamd init`. See docs/x-identity-spec.md §15.
func DefaultChainIdentity() ChainIdentity {
	return ChainIdentity{
		ChainHumanName:       "Sparkdream",
		ChainTickerPrefix:    "SDR",
		BondDenom:            "uspark.sparkdream",
		BondDisplaySymbol:    "SPARK",
		BondDisplayName:      "Sparkdream Spark",
		BondDisplayDecimals:  6,
		DreamDenom:           "udream.sparkdream",
		DreamDisplaySymbol:   "DREAM",
		DreamDisplayName:     "Sparkdream Dream",
		DreamDisplayDecimals: 6,
		// Deterministic build-time anchor; operators can override via
		// `genesis identity init --founded-at <unix>`.
		FoundedAt: 1735689600, // 2025-01-01 UTC
	}
}
