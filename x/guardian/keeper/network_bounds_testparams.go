//go:build !mainnet && !testnet && !devnet

package keeper

// The untagged test build enforces the production (mainnet) floors so the
// unit tests exercise the strict values; no e2e suite retunes gov params
// through guardian.
const activeBoundsNetwork = "mainnet"
