//go:build !mainnet && !testnet && !devnet

package types

// defaultAllowChainIdMismatch is true on local and test builds, whose chains
// use arbitrary chain ids (e2e, simulation, ignite).
const defaultAllowChainIdMismatch = true
