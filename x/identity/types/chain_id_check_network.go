//go:build mainnet || testnet || devnet

package types

// defaultAllowChainIdMismatch is false on network builds: every network's
// chain id derives from its chain_human_name (sparkdream-1 / Sparkdream,
// sparkdream-test-1 / SparkdreamTest, sparkdream-dev-1 / SparkdreamDev), so
// the soft consistency check of §11.1 runs at InitGenesis (IDN-11).
const defaultAllowChainIdMismatch = false
