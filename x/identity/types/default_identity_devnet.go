//go:build devnet

package types

// DefaultChainIdentity for the devnet build (`-tags devnet`). Distinct
// denoms AND distinct tickers from mainnet and testnet: devnet's tokens
// are SPARZ / DRMZ, not SPARK / DREAM, so a devnet voucher is
// unmistakable across IBC vouchers, indexers, and explorers even when
// the denom string itself is truncated in a UI. Devnet is the chain that
// exercises sister-chain token naming, so it gets its own symbols rather
// than reusing the canonical ones.
//
// Operators running the canonical `sparkdream-dev-1` devnet build with
// this tag get a working federated genesis with no manual `genesis
// identity init` step.
//
// These values must stay in step with
// deploy/config/network/devnet/{config.yml,genesis.json,chain.env}. The
// crossnetwork chain.env tests fail if the denom drifts from the
// committed genesis, and deploy/scripts/regenerate-network-genesis.py is
// what regenerates that genesis from this build tag.
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
		BondDenom:            "usparz.sparkdreamdev",
		BondDisplaySymbol:    "SPARZ",
		BondDisplayName:      "Sparz",
		BondDisplayDecimals:  6,
		DreamDenom:           "udrmz.sparkdreamdev",
		DreamDisplaySymbol:   "DRMZ",
		DreamDisplayName:     "Drmz",
		DreamDisplayDecimals: 6,
		FoundedAt:            1735689600,
	}
}
